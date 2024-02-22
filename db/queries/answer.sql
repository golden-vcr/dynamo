-- name: RecordAnswer :exec
insert into dynamo.answer (
    image_request_id,
    prompt,
    value
) values (
    sqlc.arg('image_request_id'),
    sqlc.arg('prompt'),
    sqlc.arg('value')
);
