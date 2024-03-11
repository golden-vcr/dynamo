begin;

alter table dynamo.image
    add column color text;

comment on column dynamo.image.color is
    'Hash-prefixed hex RGB value, e.g. "#fcee99", indicating the dominant color in the '
    'image.';

update dynamo.image set color = '#000000' where image.color is null;

alter table dynamo.image
    alter column color set not null;

commit;
