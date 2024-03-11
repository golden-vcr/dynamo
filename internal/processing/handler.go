package processing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/golden-vcr/auth"
	"github.com/golden-vcr/dynamo/gen/queries"
	"github.com/golden-vcr/dynamo/internal/discord"
	"github.com/golden-vcr/dynamo/internal/filters"
	"github.com/golden-vcr/dynamo/internal/generation"
	"github.com/golden-vcr/dynamo/internal/storage"
	"github.com/golden-vcr/ledger"
	"github.com/golden-vcr/schemas/core"
	genreq "github.com/golden-vcr/schemas/generation-requests"
	eonscreen "github.com/golden-vcr/schemas/onscreen-events"
	"github.com/golden-vcr/server-common/rmq"
	"github.com/google/uuid"
	"golang.org/x/exp/slog"
)

const ImageAlertType = "image-generation"
const ImageAlertPointsCost = 200

type Handler interface {
	Handle(ctx context.Context, logger *slog.Logger, r *genreq.Request) error
}

func NewHandler(q *queries.Queries, generationClient generation.Client, filterRunner filters.Runner, storageClient storage.Client, authServiceClient auth.ServiceClient, ledgerClient ledger.Client, onscreenEventsProducer rmq.Producer, discordWebhookUrl string) Handler {
	return &handler{
		q:                      q,
		generationClient:       generationClient,
		filterRunner:           filterRunner,
		storageClient:          storageClient,
		authServiceClient:      authServiceClient,
		ledgerClient:           ledgerClient,
		onscreenEventsProducer: onscreenEventsProducer,
		discordWebhookUrl:      discordWebhookUrl,
	}
}

type handler struct {
	q                      Queries
	generationClient       generation.Client
	filterRunner           filters.Runner
	storageClient          storage.Client
	authServiceClient      auth.ServiceClient
	ledgerClient           ledger.Client
	onscreenEventsProducer rmq.Producer
	discordWebhookUrl      string
}

func (h *handler) Handle(ctx context.Context, logger *slog.Logger, r *genreq.Request) error {
	switch r.Type {
	case genreq.RequestTypeImage:
		return h.handleImageRequest(ctx, logger, &r.Viewer, &r.State, r.Payload.Image)
	}
	return nil
}

func (h *handler) handleImageRequest(ctx context.Context, logger *slog.Logger, viewer *core.Viewer, state *core.State, payload *genreq.PayloadImage) error {
	// Get an access token from the auth service that'll allow us to deduct points from
	// the target viewer's balance
	accessToken, err := h.authServiceClient.RequestServiceToken(ctx, auth.ServiceTokenRequest{
		Service: "dynamo",
		User: auth.UserDetails{
			Id:          viewer.TwitchUserId,
			Login:       strings.ToLower(viewer.TwitchDisplayName),
			DisplayName: viewer.TwitchDisplayName,
		},
	})
	if err != nil {
		return err
	}

	// Contact the ledger service to create a pending transaction, ensuring that we can
	// deduct the requisite number of points for this generation request
	imageRequestId := uuid.New()
	alertMetadata := json.RawMessage([]byte(fmt.Sprintf(`{"imageRequestId":"%s","style":"%s"}`, imageRequestId, payload.Style)))
	transaction, err := h.ledgerClient.RequestAlertRedemption(ctx, accessToken, ImageAlertPointsCost, string(ImageAlertType), &alertMetadata)
	if err != nil {
		return err
	}
	defer transaction.Finalize(ctx)

	// Record our image generation request in the database, and prepare a function that
	// we can use to record its failure (prior to returning) in the event of any error
	broadcastId := sql.NullInt32{}
	if state.BroadcastId != 0 {
		broadcastId.Valid = true
		broadcastId.Int32 = int32(state.BroadcastId)
	}
	screeningId := uuid.NullUUID{}
	if state.ScreeningId != uuid.Nil {
		screeningId.Valid = true
		screeningId.UUID = state.ScreeningId
	}
	inputs, err := json.Marshal(payload.Inputs)
	if err != nil {
		return err
	}
	description := formatDescription(payload.Style, payload.Inputs)
	prompt := formatPrompt(payload.Style, payload.Inputs)
	if err := h.q.RecordImageRequest(ctx, queries.RecordImageRequestParams{
		ImageRequestID: imageRequestId,
		TwitchUserID:   viewer.TwitchUserId,
		BroadcastID:    broadcastId,
		ScreeningID:    screeningId,
		Style:          string(payload.Style),
		Inputs:         inputs,
		Prompt:         prompt,
	}); err != nil {
		return err
	}
	recordFailure := func(err error) error {
		_, dbErr := h.q.RecordImageRequestFailure(ctx, queries.RecordImageRequestFailureParams{
			ImageRequestID: imageRequestId,
			ErrorMessage:   err.Error(),
		})
		return dbErr
	}

	// If this is a friend request, obtain an AI-generated name for our new friend
	imageType := eonscreen.ImageTypeGhost
	generatedText := ""
	if payload.Style == genreq.ImageStyleFriend {
		imageType = eonscreen.ImageTypeFriend
		friendNamePrompt := fmt.Sprintf("Please come up with a name for a friendly mascot character who is %s. Please answer with a single name, and no additional text.", payload.Inputs.Friend.Subject)
		friendName, err := h.generationClient.GenerateText(ctx, friendNamePrompt, viewer.TwitchUserId)
		if err != nil {
			recordFailure(fmt.Errorf("error in text generation: %w", err))
			return err
		}
		if err := h.q.RecordAnswer(ctx, queries.RecordAnswerParams{
			ImageRequestID: imageRequestId,
			Prompt:         friendNamePrompt,
			Value:          friendName,
		}); err != nil {
			recordFailure(err)
			return err
		}
		generatedText = friendName
	}

	// Generate a new image, waiting until it's ready
	image, err := h.generationClient.GenerateImage(ctx, prompt, viewer.TwitchUserId)
	if err != nil {
		recordFailure(err)
		return err
	}

	// If the image needs its background removed, use our remove-background routine from
	// the image-filters library to detect the background color and key it out,
	// producing a compressed WEBP image with a transparent background
	backgroundColor := "#000000"
	if payload.Style == genreq.ImageStyleFriend {
		// For a friend image, use an external utility to convert from PNG to WEBP,
		// keying out the background in the process
		basename := fmt.Sprintf("imf_%s", imageRequestId)

		// Write the PNG to disk temporarily so it can be processed by another program
		infile, err := os.CreateTemp("", basename+".png")
		if err != nil {
			recordFailure(err)
			return err
		}
		defer infile.Close()
		defer os.Remove(infile.Name())
		if _, err := infile.Write(image.Data); err != nil {
			recordFailure(err)
			return err
		}
		infile.Close()

		// Build the path to our processed WEBP file
		outfileName := strings.TrimSuffix(infile.Name(), filepath.Ext(infile.Name())) + ".webp"
		defer os.Remove(outfileName)

		// Invoke 'imf remove-background -i <infile> -o <outfile>' to write a new image,
		// capturing the detected background color
		color, err := h.filterRunner.RemoveBackground(ctx, infile.Name(), outfileName)
		if err != nil {
			recordFailure(err)
			return err
		}
		backgroundColor = color

		// Read the newly-written WEBP file from disk to get our final image data
		webpData, err := os.ReadFile(outfileName)
		if err != nil {
			recordFailure(err)
			return err
		}
		image.ContentType = "image/webp"
		image.Data = webpData
	} else {
		// For images that don't need to be processed with image-filters, convert from
		// PNG to JPEG in-memory
		bmpData, err := png.Decode(bytes.NewReader(image.Data))
		if err != nil {
			err = fmt.Errorf("failed to decode PNG data for OpenAI-hosted image: %w", err)
			recordFailure(err)
			return err
		}

		// Preallocate a buffer that's roughly as large as the largest 1024x1024 JPEG
		// we can reasonably expect to produce, then write our compressed JPEG data into
		// it
		jpegBuffer := bytes.NewBuffer(make([]byte, 0, 512*1024))
		if err := jpeg.Encode(jpegBuffer, bmpData, &jpeg.Options{Quality: 80}); err != nil {
			err = fmt.Errorf("failed to encode JPEG image from decoded PNG image: %w", err)
			recordFailure(err)
			return err
		}

		// Replace the image with our compressed JPEG version
		image.ContentType = "image/jpeg"
		image.Data = jpegBuffer.Bytes()
	}

	// Store the resulting image in our S3-compatible bucket, for posterity and so it
	// can be served to the alerts overlay
	imageUrl, err := storeImage(ctx, imageRequestId, h.q, h.storageClient, image, backgroundColor)
	if err != nil {
		recordFailure(err)
		return err
	}

	// Flag the image generation request as successful, since we've now generated all
	// required assets
	if _, err := h.q.RecordImageRequestSuccess(ctx, imageRequestId); err != nil {
		return err
	}

	// Generate an alert that will display the image onscreen during the stream
	ev := eonscreen.Event{
		Type: eonscreen.EventTypeImage,
		Payload: eonscreen.Payload{
			Image: &eonscreen.PayloadImage{
				Type:    imageType,
				Viewer:  *viewer,
				Details: eonscreen.ImageDetails{},
			},
		},
	}
	switch ev.Payload.Image.Type {
	case eonscreen.ImageTypeGhost:
		ev.Payload.Image.Details.Ghost = &eonscreen.ImageDetailsGhost{
			ImageUrl:    imageUrl,
			Description: description,
		}
	case eonscreen.ImageTypeFriend:
		ev.Payload.Image.Details.Friend = &eonscreen.ImageDetailsFriend{
			ImageUrl:        imageUrl,
			Description:     description,
			Name:            generatedText,
			BackgroundColor: backgroundColor,
		}
	default:
		return fmt.Errorf("unhandled image type")
	}
	if err := h.produceOnscreenEvent(ctx, logger, ev); err != nil {
		recordFailure(err)
		return err
	}

	// We've successfully generated an alert from the user's request, so finalize the
	// transaction to deduct the points we debited from them - if we don't make it here,
	// our deferred called to transaction.Finalize will reject the transaction instead,
	// causing the debited points to be refunded
	if err := transaction.Accept(ctx); err != nil {
		return fmt.Errorf("failed to finalize transaction: %w", err)
	}

	// Don't hold up the request to do this; just initiate a fire-and-forget HTTP
	// request to a Discord webhook, so that we can post this image to our #ghosts
	// channel in the Discord server. If the request fails, we'll simply print an error.
	if h.discordWebhookUrl != "" && payload.Style == genreq.ImageStyleGhost {
		go func() {
			err := discord.PostGhostAlert(h.discordWebhookUrl, viewer.TwitchDisplayName, description, imageUrl)
			if err != nil {
				logger.Error("ERROR: Failed to post ghost alert to Discord", "error", err)
			}
		}()
	}

	return nil
}

func formatDescription(style genreq.ImageStyle, inputs genreq.ImageInputs) string {
	switch style {
	case genreq.ImageStyleGhost:
		return inputs.Ghost.Subject
	case genreq.ImageStyleFriend:
		return inputs.Friend.Subject
	}
	return "an image"
}

func formatPrompt(style genreq.ImageStyle, inputs genreq.ImageInputs) string {
	switch style {
	case genreq.ImageStyleGhost:
		return fmt.Sprintf("a ghostly image of %s, with glitchy VHS artifacts, dark background", inputs.Ghost.Subject)
	case genreq.ImageStyleFriend:
		color := inputs.Friend.Color
		backgroundColor := inputs.Friend.Color.GetComplement()

		article := ""
		subject := inputs.Friend.Subject
		if strings.HasPrefix(subject, "a ") {
			if color[0] == 'a' || color[0] == 'e' || color[0] == 'i' || color[0] == 'o' || color[0] == 'u' {
				article = "an"
			} else {
				article = "a"
			}
			subject = subject[2:]
		} else if strings.HasPrefix(subject, "an ") {
			if color[0] == 'a' || color[0] == 'e' || color[0] == 'i' || color[0] == 'o' || color[0] == 'u' {
				article = "an"
			} else {
				article = "a"
			}
			subject = subject[3:]
		} else if strings.HasPrefix(subject, "the ") {
			subject = subject[4:]
		}

		return fmt.Sprintf("%s %s %s, illustrated in the style of 1990s digital clip art images, with a limited 256-color palette and sharp black outlines, with a solid %s background suitable for chroma keying",
			article,
			color,
			subject,
			backgroundColor,
		)
	}
	return "a sign that says BAD STYLE, UNABLE TO FORMAT PROMPT"
}

func formatImageKey(imageRequestId uuid.UUID, contentType string) string {
	ext := ".jpg"
	if contentType == "image/png" {
		ext = ".png"
	} else if contentType == "image/webp" {
		ext = ".webp"
	}
	return fmt.Sprintf("%s/%s-0%s", imageRequestId, imageRequestId, ext)
}

func storeImage(ctx context.Context, imageRequestId uuid.UUID, q Queries, storageClient storage.Client, image *generation.Image, color string) (string, error) {
	// Store the image in our S3-compatible bucket
	key := formatImageKey(imageRequestId, image.ContentType)
	imageUrl, err := storageClient.Upload(ctx, key, image.ContentType, bytes.NewReader(image.Data))
	if err != nil {
		return "", fmt.Errorf("failed to upload generated image to storage: %w", err)
	}

	// Record the fact that we've received this generated image
	if err := q.RecordImage(ctx, queries.RecordImageParams{
		ImageRequestID: imageRequestId,
		Index:          0,
		Url:            imageUrl,
		Color:          color,
	}); err != nil {
		return "", fmt.Errorf("failed to record newly-stored image URL in database: %w", err)
	}
	return imageUrl, nil
}
