begin;

alter table dynamo.image
    drop column color;

commit;
