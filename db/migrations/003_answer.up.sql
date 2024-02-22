begin;

create table dynamo.answer (
    image_request_id uuid not null,
    prompt           text not null,
    value            text not null
);

comment on table dynamo.answer is
    'Record of a string value that was obtained via a text generation API in the '
    'context of a request';
comment on column dynamo.answer.image_request_id is
    'ID of the image_request record associated with this answer.';
comment on column dynamo.answer.prompt is
    'Complete prompt that was used to obtain this answer.';
comment on column dynamo.answer.value is
    'String value obtained by prompting a language model.';

alter table dynamo.answer
    add constraint image_request_id_fk
    foreign key (image_request_id) references dynamo.image_request (id);

commit;
