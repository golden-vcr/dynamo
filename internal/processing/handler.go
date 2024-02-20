package processing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golden-vcr/auth"
	"github.com/golden-vcr/dynamo/gen/queries"
	"github.com/golden-vcr/dynamo/internal/discord"
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

func NewHandler(q *queries.Queries, generationClient generation.Client, storageClient storage.Client, authServiceClient auth.ServiceClient, ledgerClient ledger.Client, onscreenEventsProducer rmq.Producer, discordWebhookUrl string) Handler {
	return &handler{
		q:                      q,
		generationClient:       generationClient,
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

	// Generate a new image, waiting until it's ready
	imageType := generation.ImageTypeScreen
	if payload.Style == genreq.ImageStyleClipArt {
		imageType = generation.ImageTypeTransparent
	}
	image, err := h.generationClient.GenerateImage(ctx, prompt, viewer.TwitchUserId, imageType)
	if err != nil {
		recordFailure(err)
		return err
	}

	// Store the resulting image in our S3-compatible bucket, for posterity and so it
	// can be served to the alerts overlay
	imageUrl, err := storeImage(ctx, imageRequestId, h.q, h.storageClient, image)
	if err != nil {
		recordFailure(err)
		return err
	}

	// Flag the image generation requests as successful, since we've now generated all
	// required images
	if _, err := h.q.RecordImageRequestSuccess(ctx, imageRequestId); err != nil {
		return err
	}

	// Generate an alert that will display the image onscreen during the stream
	if err := h.produceOnscreenEvent(ctx, logger, eonscreen.Event{
		Type: eonscreen.EventTypeImage,
		Payload: eonscreen.Payload{
			Image: &eonscreen.PayloadImage{
				Viewer:      *viewer,
				Style:       payload.Style,
				Description: description,
				ImageUrl:    imageUrl,
			},
		},
	}); err != nil {
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
	}
	return "an image"
}

func formatPrompt(style genreq.ImageStyle, inputs genreq.ImageInputs) string {
	switch style {
	case genreq.ImageStyleGhost:
		return fmt.Sprintf("a ghostly image of %s, with glitchy VHS artifacts, dark background", inputs.Ghost.Subject)
	case genreq.ImageStyleClipArt:
		color := inputs.ClipArt.Color
		backgroundColor := inputs.ClipArt.Color.GetComplement()
		article := "a"
		if len(color) > 0 && (color[0] == 'a' || color[0] == 'e' || color[0] == 'i' || color[0] == 'o' || color[0] == 'u') {
			article = "an"
		}
		return fmt.Sprintf("%s %s %s, illustrated in the style of 1990s digital clip art images, with a limited 256-color palette and sharp black outlines, with a solid %s background suitable for chroma keying",
			article,
			color,
			inputs.ClipArt.Subject,
			backgroundColor,
		)
	}
	return "a sign that says BAD STYLE, UNABLE TO FORMAT PROMPT"
}

func formatImageKey(imageRequestId uuid.UUID, index int) string {
	return fmt.Sprintf("%s/%s-%02d.jpg", imageRequestId, imageRequestId, index)
}

func storeImage(ctx context.Context, imageRequestId uuid.UUID, q Queries, storageClient storage.Client, image *generation.Image) (string, error) {
	// Store the image in our S3-compatible bucket
	key := formatImageKey(imageRequestId, 0)
	imageUrl, err := storageClient.Upload(ctx, key, image.ContentType, bytes.NewReader(image.Data))
	if err != nil {
		return "", fmt.Errorf("failed to upload generated image to storage: %w", err)
	}

	// Record the fact that we've received this generated image
	if err := q.RecordImage(ctx, queries.RecordImageParams{
		ImageRequestID: imageRequestId,
		Index:          0,
		Url:            imageUrl,
	}); err != nil {
		return "", fmt.Errorf("failed to record newly-stored image URL in database: %w", err)
	}
	return imageUrl, nil
}
