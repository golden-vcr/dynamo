package processing

import (
	"context"
	"database/sql"

	"github.com/golden-vcr/dynamo/gen/queries"
	"github.com/google/uuid"
)

type Queries interface {
	RecordImageRequest(ctx context.Context, arg queries.RecordImageRequestParams) error
	RecordImageRequestFailure(ctx context.Context, arg queries.RecordImageRequestFailureParams) (sql.Result, error)
	RecordImageRequestSuccess(ctx context.Context, imageRequestID uuid.UUID) (sql.Result, error)
	RecordImage(ctx context.Context, arg queries.RecordImageParams) error
	RecordAnswer(ctx context.Context, arg queries.RecordAnswerParams) error
}
