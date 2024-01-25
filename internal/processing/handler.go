package processing

import (
	"context"
	"fmt"

	"github.com/golden-vcr/auth"
	"github.com/golden-vcr/ledger"
	genreq "github.com/golden-vcr/schemas/generation-requests"
	etwitch "github.com/golden-vcr/schemas/twitch-events"
	"github.com/golden-vcr/server-common/rmq"
	"golang.org/x/exp/slog"
)

type Handler interface {
	Handle(ctx context.Context, logger *slog.Logger, r *genreq.Request) error
}

func NewHandler(authServiceClient auth.ServiceClient, ledgerClient ledger.Client, onscreenEventsProducer rmq.Producer) Handler {
	return &handler{
		authServiceClient:      authServiceClient,
		ledgerClient:           ledgerClient,
		onscreenEventsProducer: onscreenEventsProducer,
	}
}

type handler struct {
	authServiceClient          auth.ServiceClient
	ledgerClient               ledger.Client
	onscreenEventsProducer     rmq.Producer
	generationRequestsProducer rmq.Producer
}

func (h *handler) Handle(ctx context.Context, logger *slog.Logger, r *genreq.Request) error {
	switch r.Type {
	case genreq.RequestTypeImage:
		return h.handleImageRequest(ctx, logger, &r.Viewer, r.Payload.Image)
	}
	return nil
}

func (h *handler) handleImageRequest(ctx context.Context, logger *slog.Logger, viewer *etwitch.Viewer, payload *genreq.PayloadImage) error {
	return fmt.Errorf("NYI")
}
