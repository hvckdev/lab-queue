#!/bin/sh
set -eu

# Optional local helper; Compose loads the SQL directly without executing a
# bind-mounted shell script. psql reads and safely quotes the password variables.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 --file="$script_dir/init-databases.sql"
