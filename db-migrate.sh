#!/usr/bin/env bash
set -e

GOLANG_MIGRATE_VERSION=v4.16.2

function require_env() {
    NAME="$1"
    VALUE="$2"
    if [ "$VALUE" == "" ]; then
        echo "ERROR: $NAME must be set"
        exit 1
    fi
}

function url_encode() {
    python3 -c 'import sys; import urllib.parse; print(urllib.parse.quote_plus(sys.argv[1]))' "$1"
}

if [ "$PGHOST" == "" ] || [ "$PGPORT" == "" ] || [ "$PGDATABASE" == "" ] || [ "$PGUSER" == "" ] || [ "$PGPASSWORD" == "" ]; then
    if [ -f .env ]; then
        source .env
    fi
    if [ -f bin/.env ]; then
        source bin/.env
    fi
fi
require_env 'PGHOST' "$PGHOST"
require_env 'PGPORT' "$PGPORT"
require_env 'PGDATABASE' "$PGDATABASE"
require_env 'PGUSER' "$PGUSER"
require_env 'PGPASSWORD' "$PGPASSWORD"

SCHEMA_NAME=$(basename $(pwd))
POSTGRES_URI="postgres://$PGUSER:$(url_encode "$PGPASSWORD")@$PGHOST:$PGPORT/$PGDATABASE?search_path=public&x-migrations-table=${SCHEMA_NAME}_migrations"
if [ "$PGSSLMODE" != "" ]; then
    POSTGRES_URI="$POSTGRES_URI&sslmode=$PGSSLMODE"
fi

MIGRATE_COMMAND="$@"
if [ "$MIGRATE_COMMAND" == "" ]; then
    MIGRATE_COMMAND="up"
fi

go run \
    -tags 'postgres' \
    "github.com/golang-migrate/migrate/v4/cmd/migrate@$GOLANG_MIGRATE_VERSION" \
    -source file://db/migrations \
    -database "$POSTGRES_URI" \
    $MIGRATE_COMMAND
