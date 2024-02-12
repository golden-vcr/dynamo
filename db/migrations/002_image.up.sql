begin;

create table dynamo.image_request (
    id                  uuid primary key,
    twitch_user_id      text not null,
    broadcast_id        integer,
    screening_id        uuid,

    style               text not null,
    inputs              jsonb not null,
    prompt              text not null,

    created_at          timestamptz not null default now(),
    finished_at         timestamptz,
    error_message       text
);

comment on table dynamo.image_request is
    'Records the fact that a user requested that images be generated, with their '
    'chosen prompt, to be overlaid on the video during the stream.';
comment on column dynamo.image_request.id is
    'Globally unique identifier for this request.';
comment on column dynamo.image_request.twitch_user_id is
    'ID of the Twitch user that initiated this request.';
comment on column dynamo.image_request.broadcast_id is
    'ID of the broadcast that was active when the image request was submitted; may be '
    'null if the request was submitted while no broadcast was in progress.';
comment on column dynamo.image_request.screening_id is
    'ID of the screening that was active when the image request was submitted; may be '
    'null if the request was submitted while no tape was active.';
comment on column dynamo.image_request.style is
    'The style of image being requested, from the generation-requests schema.';
comment on column dynamo.image_request.inputs is
    'JSON object containing user-supplied inputs describing the desired image, e.g. '
    '{"subject":"a cardboard box"}, {"subject":"several large turkeys"}, '
    '{"subject":"the concept of love"}';
comment on column dynamo.image_request.prompt is
    'The complete prompt that was submitted in order to initiate image generation, '
    'e.g. "a ghostly image of several large turkeys, with glitchy VHS artifacts, dark '
    'background".';
comment on column dynamo.image_request.created_at is
    'Timestamp indicating when the request was submitted.';
comment on column dynamo.image_request.finished_at is
    'Timestamp indicating when we received a response for the image generation '
    'request, whether successful or not. If NULL, the request is still being processed '
    'and images are not ready yet.';
comment on column dynamo.image_request.error_message is
    'Error message describing why the request completed unsuccessfully. If NULL and '
    'finished_at is not NULL, the request completed successfully.';

create table dynamo.image (
    image_request_id uuid not null,
    index            integer not null,
    url              text not null
);

comment on table dynamo.image is
    'Record of an image that was successfully generated from a user-submitted image '
    'request. An image request may result in multiple images. Images are ordered by '
    'index, matching the array in which they were returned by the image generation '
    'API.';
comment on column dynamo.image.image_request_id is
    'ID of the image_request record associated with this image.';
comment on column dynamo.image.index is
    'Sequential, zero-indexed position of this image in the original results array.';
comment on column dynamo.image.url is
    'URL indicating where the image has been uploaded for long-term storage, so that '
    'it can be displayed in client applications.';

alter table dynamo.image
    add constraint image_request_id_fk
    foreign key (image_request_id) references dynamo.image_request (id);

alter table dynamo.image
    add constraint image_request_id_index_unique
    unique (image_request_id, index);

commit;
