-- name: RecordImageRequest :exec
insert into dynamo.image_request (
    id,
    twitch_user_id,
    broadcast_id,
    screening_id,
    style,
    inputs,
    prompt,
    created_at
) values (
    sqlc.arg('image_request_id'),
    sqlc.arg('twitch_user_id'),
    sqlc.narg('broadcast_id'),
    sqlc.narg('screening_id'),
    sqlc.arg('style'),
    sqlc.arg('inputs'),
    sqlc.arg('prompt'),
    now()
);

-- name: RecordImageRequestFailure :execresult
update dynamo.image_request set
    finished_at = now(),
    error_message = sqlc.arg('error_message')::text
where image_request.id = sqlc.arg('image_request_id')
    and finished_at is null;

-- name: RecordImageRequestSuccess :execresult
update dynamo.image_request set
    finished_at = now()
where image_request.id = sqlc.arg('image_request_id')
    and finished_at is null;

-- name: RecordImage :exec
insert into dynamo.image (
    image_request_id,
    index,
    url,
    color
) values (
    sqlc.arg('image_request_id'),
    sqlc.arg('index'),
    sqlc.arg('url'),
    sqlc.arg('color')
);
