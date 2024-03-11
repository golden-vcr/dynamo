package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/codingconcepts/env"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/sync/errgroup"

	"github.com/golden-vcr/auth"
	"github.com/golden-vcr/dynamo/gen/queries"
	"github.com/golden-vcr/dynamo/internal/filters"
	"github.com/golden-vcr/dynamo/internal/generation"
	"github.com/golden-vcr/dynamo/internal/processing"
	"github.com/golden-vcr/dynamo/internal/storage"
	"github.com/golden-vcr/ledger"
	genreq "github.com/golden-vcr/schemas/generation-requests"
	"github.com/golden-vcr/server-common/db"
	"github.com/golden-vcr/server-common/entry"
	"github.com/golden-vcr/server-common/rmq"
)

type Config struct {
	AuthURL          string `env:"AUTH_URL" default:"http://localhost:5002"`
	AuthSharedSecret string `env:"AUTH_SHARED_SECRET" required:"true"`
	LedgerURL        string `env:"LEDGER_URL" default:"http://localhost:5003"`

	OpenaiApiKey string `env:"OPENAI_API_KEY" required:"true"`

	DiscordGhostsWebhookUrl string `env:"DISCORD_GHOSTS_WEBHOOK_URL"`

	SpacesBucketName     string `env:"SPACES_BUCKET_NAME" required:"true"`
	SpacesRegionName     string `env:"SPACES_REGION_NAME" required:"true"`
	SpacesEndpointOrigin string `env:"SPACES_ENDPOINT_URL" required:"true"`
	SpacesAccessKeyId    string `env:"SPACES_ACCESS_KEY_ID" required:"true"`
	SpacesSecretKey      string `env:"SPACES_SECRET_KEY" required:"true"`

	RmqHost     string `env:"RMQ_HOST" required:"true"`
	RmqPort     int    `env:"RMQ_PORT" required:"true"`
	RmqVhost    string `env:"RMQ_VHOST" required:"true"`
	RmqUser     string `env:"RMQ_USER" required:"true"`
	RmqPassword string `env:"RMQ_PASSWORD" required:"true"`

	DatabaseHost     string `env:"PGHOST" required:"true"`
	DatabasePort     int    `env:"PGPORT" required:"true"`
	DatabaseName     string `env:"PGDATABASE" required:"true"`
	DatabaseUser     string `env:"PGUSER" required:"true"`
	DatabasePassword string `env:"PGPASSWORD" required:"true"`
	DatabaseSslMode  string `env:"PGSSLMODE"`
}

func main() {
	app, ctx := entry.NewApplication("dynamo-consumer")
	defer app.Stop()

	// Parse config from environment variables
	err := godotenv.Load()
	if err != nil && !os.IsNotExist(err) {
		app.Fail("Failed to load .env file", err)
	}
	config := Config{}
	if err := env.Set(&config); err != nil {
		app.Fail("Failed to load config", err)
	}

	// Resolve our 'imf' command-line tool from the PATH, since we need it to process
	// some generated images (see https://github.com/golden-vcr/image-filters: for the
	// time being we invoke the imf binary as a subprocess rather than linking the
	// OpenCV-dependent static library into this executable with cgo)
	imfBinaryPath := ""
	if _, err := exec.LookPath("imf"); err == nil {
		imfBinaryPath = "imf"
	} else {
		binaryName := "imf"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		wd, err := os.Getwd()
		if err != nil {
			app.Fail("Failed to get cwd", err)
		}
		fromRoot, err := filepath.Abs(filepath.Join(wd, "external", "bin", binaryName))
		if err != nil {
			app.Fail("Failed to construct path", err)
		}
		fromBin, err := filepath.Abs(filepath.Join(wd, "..", "external", "bin", binaryName))
		if err != nil {
			app.Fail("Failed to construct path", err)
		}
		for _, binaryPath := range []string{fromRoot, fromBin} {
			fi, err := os.Stat(binaryPath)
			if err == nil && !fi.IsDir() {
				imfBinaryPath = binaryPath
				break
			}
		}
	}
	if imfBinaryPath == "" {
		app.Fail("imf is not in the PATH and was not found relative to cwd in external/bin", err)
	}
	filterRunner := filters.NewRunner(app.Log(), imfBinaryPath)

	// Configure our database connection and initialize a Queries struct, so we can use
	// and the 'dynamo' schema to record data about image generation requests
	connectionString := db.FormatConnectionString(
		config.DatabaseHost,
		config.DatabasePort,
		config.DatabaseName,
		config.DatabaseUser,
		config.DatabasePassword,
		config.DatabaseSslMode,
	)
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		app.Fail("Failed to open sql.DB", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		app.Fail("Failed to connect to database", err)
	}
	q := queries.New(db)

	// We need an auth service client so that when we can obtain JWTs that will
	// authorize us to debit fun points from users in exchange for alerts, which we
	// accomplish with a ledger client
	authServiceClient := auth.NewServiceClient(config.AuthURL, config.AuthSharedSecret)
	ledgerClient := ledger.NewClient(config.LedgerURL)

	// Initialize an AMQP client
	amqpConn, err := amqp.Dial(rmq.FormatConnectionString(config.RmqHost, config.RmqPort, config.RmqVhost, config.RmqUser, config.RmqPassword))
	if err != nil {
		app.Fail("Failed to connect to AMQP server", err)
	}
	defer amqpConn.Close()

	// Prepare a producer that we can use to send messages to the onscreen-events queue,
	// whenenver we're finishing generating assets and are ready to move on to using
	// them in alerts
	onscreenEventsProducer, err := rmq.NewProducer(amqpConn, "onscreen-events")
	if err != nil {
		app.Fail("Failed to initialize AMQP producer for onscreen-events", err)
	}

	// Prepare a consumer and start receiving incoming messages from the
	// generation-requests exchange
	generationEventsConsumer, err := rmq.NewConsumer(amqpConn, "generation-requests")
	if err != nil {
		app.Fail("Failed to initialize AMQP consumer for generation-events", err)
	}
	generationRequests, err := generationEventsConsumer.Recv(ctx)
	if err != nil {
		app.Fail("Failed to init recv channel on generation-events consumer", err)
	}

	// Prepare our internal generation.Client and storage.Client interfaces, which allow
	// us to generate assets and store them in S3, respectively
	generationClient := generation.NewClient(config.OpenaiApiKey)
	storageClient, err := storage.NewClient(config.SpacesAccessKeyId, config.SpacesSecretKey, config.SpacesEndpointOrigin, config.SpacesRegionName, config.SpacesBucketName)
	if err != nil {
		app.Fail("Failed to initialize storage client", err)
	}

	// Prepare a handler that has the state necessary to respond to incoming
	// generation-requests messages by initiating external requests to generate the
	// required assets, debiting points from the user in the process, then producing to
	// the onscreen-events queue to use those assets in alerts
	h := processing.NewHandler(
		q,
		generationClient,
		filterRunner,
		storageClient,
		authServiceClient,
		ledgerClient,
		onscreenEventsProducer,
		config.DiscordGhostsWebhookUrl,
	)

	// Each time we read a message from the queue, spin up a new goroutine for that
	// message, parse it according to our generation-requests schema, then handle it
	wg, ctx := errgroup.WithContext(ctx)
	done := false
	for !done {
		select {
		case <-ctx.Done():
			app.Log().Info("Consumer context canceled; exiting main loop")
			done = true
		case d, ok := <-generationRequests:
			if ok {
				wg.Go(func() error {
					var r genreq.Request
					if err := json.Unmarshal(d.Body, &r); err != nil {
						return err
					}
					logger := app.Log().With("generationRequest", r)
					logger.Info("Consumed from generation-requests")
					if err := h.Handle(ctx, logger, &r); err != nil {
						logger.Info("Failed to handle event", "error", err)
					}
					return err
				})
			} else {
				app.Log().Info("Channel is closed; exiting main loop")
				done = true
			}
		}
	}

	if err := wg.Wait(); err != nil {
		app.Fail("Encountered an error during message handling", err)
	}
}
