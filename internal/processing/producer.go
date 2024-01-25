package processing

import (
	"context"
	"encoding/json"

	"golang.org/x/exp/slog"

	eonscreen "github.com/golden-vcr/schemas/onscreen-events"
)

func (h *handler) produceOnscreenEvent(ctx context.Context, logger *slog.Logger, ev eonscreen.Event) error {
	logger = logger.With("onscreenEvent", ev)
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	err = h.onscreenEventsProducer.Send(ctx, data)
	if err != nil {
		logger.Error("Failed to produce to onscreen-events")
	} else {
		logger.Info("Produced to onscreen-events")
	}
	return err
}
