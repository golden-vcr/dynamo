package queries_test

import (
	"context"
	"testing"

	"github.com/golden-vcr/dynamo/gen/queries"
	"github.com/golden-vcr/server-common/querytest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func Test_RecordImageRequest(t *testing.T) {
	tx := querytest.PrepareTx(t)
	q := queries.New(tx)

	querytest.AssertCount(t, tx, 0, "SELECT COUNT(*) FROM dynamo.image_request")

	err := q.RecordImageRequest(context.Background(), queries.RecordImageRequestParams{
		ImageRequestID: uuid.MustParse("5e3a831b-699e-45f2-9587-048cbaeaf17d"),
		TwitchUserID:   "1005",
		Style:          "ghost",
		Inputs:         []byte(`{"subject":"a scary clown"}`),
		Prompt:         "an image of a scary clown, dark background",
	})
	assert.NoError(t, err)

	querytest.AssertCount(t, tx, 1, `
		SELECT COUNT(*) FROM dynamo.image_request
			WHERE id = '5e3a831b-699e-45f2-9587-048cbaeaf17d'
			AND twitch_user_id = '1005'
			AND style = 'ghost'
			AND inputs = '{"subject":"a scary clown"}'::jsonb
			AND prompt = 'an image of a scary clown, dark background'
			AND created_at IS NOT NULL
			AND finished_at IS NULL
			AND error_message IS NULL
	`)
}

func Test_RecordImageRequestFailure(t *testing.T) {
	tx := querytest.PrepareTx(t)
	q := queries.New(tx)

	querytest.AssertCount(t, tx, 0, "SELECT COUNT(*) FROM dynamo.image_request")

	err := q.RecordImageRequest(context.Background(), queries.RecordImageRequestParams{
		ImageRequestID: uuid.MustParse("8071fb37-8318-4eec-a479-5b329d2fb6a9"),
		TwitchUserID:   "2006",
		Style:          "ghost",
		Inputs:         []byte(`{"subject":"several geese"}`),
		Prompt:         "an image of several geese, dark background",
	})
	assert.NoError(t, err)

	res, err := q.RecordImageRequestFailure(context.Background(), queries.RecordImageRequestFailureParams{
		ImageRequestID: uuid.MustParse("8071fb37-8318-4eec-a479-5b329d2fb6a9"),
		ErrorMessage:   "something went wrong",
	})
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 1)

	querytest.AssertCount(t, tx, 1, `
		SELECT COUNT(*) FROM dynamo.image_request
			WHERE id = '8071fb37-8318-4eec-a479-5b329d2fb6a9'
			AND twitch_user_id = '2006'
			AND style = 'ghost'
			AND inputs = '{"subject":"several geese"}'::jsonb
			AND prompt = 'an image of several geese, dark background'
			AND created_at IS NOT NULL
			AND finished_at IS NOT NULL
			AND error_message = 'something went wrong'
	`)

	// Attempting to record a result for an image_request that's already finished should
	// affect 0 rows
	res, err = q.RecordImageRequestFailure(context.Background(), queries.RecordImageRequestFailureParams{
		ImageRequestID: uuid.MustParse("8071fb37-8318-4eec-a479-5b329d2fb6a9"),
		ErrorMessage:   "a different thing went wrong, like, again",
	})
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 0)

	// Attempting to record a result for an image_request with an invalid uuid should
	// affect 0 rows
	res, err = q.RecordImageRequestFailure(context.Background(), queries.RecordImageRequestFailureParams{
		ImageRequestID: uuid.MustParse("02448cd2-0663-47bd-bc5a-0296bcd27fff"),
		ErrorMessage:   "oh no",
	})
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 0)
}

func Test_RecordImageRequestSuccess(t *testing.T) {
	tx := querytest.PrepareTx(t)
	q := queries.New(tx)

	querytest.AssertCount(t, tx, 0, "SELECT COUNT(*) FROM dynamo.image_request")

	err := q.RecordImageRequest(context.Background(), queries.RecordImageRequestParams{
		ImageRequestID: uuid.MustParse("5e6115ea-d7ac-44aa-81a0-17a715bc984d"),
		TwitchUserID:   "3007",
		Style:          "ghost",
		Inputs:         []byte(`{"subject":"a platypus playing the saxaphone"}`),
		Prompt:         "an image of a platypus playing the saxaphone, dark background",
	})
	assert.NoError(t, err)

	res, err := q.RecordImageRequestSuccess(context.Background(), uuid.MustParse("5e6115ea-d7ac-44aa-81a0-17a715bc984d"))
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 1)

	querytest.AssertCount(t, tx, 1, `
		SELECT COUNT(*) FROM dynamo.image_request
			WHERE id = '5e6115ea-d7ac-44aa-81a0-17a715bc984d'
			AND twitch_user_id = '3007'
			AND style = 'ghost'
			AND inputs = '{"subject":"a platypus playing the saxaphone"}'::jsonb
			AND prompt = 'an image of a platypus playing the saxaphone, dark background'
			AND created_at IS NOT NULL
			AND finished_at IS NOT NULL
			AND error_message IS NULL
	`)

	// Attempting to record a result for an image_request that's already finished should
	// affect 0 rows
	res, err = q.RecordImageRequestSuccess(context.Background(), uuid.MustParse("5e6115ea-d7ac-44aa-81a0-17a715bc984d"))
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 0)

	// Attempting to record a result for an image_request with an invalid uuid should
	// affect 0 rows
	res, err = q.RecordImageRequestSuccess(context.Background(), uuid.MustParse("1c98937b-406d-4358-aec5-b69edd460394"))
	assert.NoError(t, err)
	querytest.AssertNumRowsChanged(t, res, 0)
}

func Test_RecordImage(t *testing.T) {
	tx := querytest.PrepareTx(t)
	q := queries.New(tx)

	querytest.AssertCount(t, tx, 0, "SELECT COUNT(*) FROM dynamo.image_request")
	querytest.AssertCount(t, tx, 0, "SELECT COUNT(*) FROM dynamo.image")

	err := q.RecordImageRequest(context.Background(), queries.RecordImageRequestParams{
		ImageRequestID: uuid.MustParse("dfaf425a-17fa-4bf1-b49b-74ce354deb6f"),
		TwitchUserID:   "4444",
		Style:          "ghost",
		Inputs:         []byte(`{"subject":"a juicy hamburger"}`),
		Prompt:         "an image of a juicy hamburger, dark background",
	})
	assert.NoError(t, err)

	err = q.RecordImage(context.Background(), queries.RecordImageParams{
		ImageRequestID: uuid.MustParse("dfaf425a-17fa-4bf1-b49b-74ce354deb6f"),
		Index:          0,
		Url:            "http://example.com/my-cool-image.png",
		Color:          "#fc99ee",
	})
	assert.NoError(t, err)

	querytest.AssertCount(t, tx, 1, `
		SELECT COUNT(*) FROM dynamo.image
			WHERE image_request_id = 'dfaf425a-17fa-4bf1-b49b-74ce354deb6f'
			AND index = 0
			AND url = 'http://example.com/my-cool-image.png'
			AND color = '#fc99ee'
	`)
}
