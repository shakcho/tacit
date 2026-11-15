#!/usr/bin/env bash
set -euo pipefail

: "${TACIT_RESTORE_SOURCE_URL:?TACIT_RESTORE_SOURCE_URL is required}"
: "${TACIT_RESTORE_TARGET_URL:?TACIT_RESTORE_TARGET_URL is required}"

if [[ "$TACIT_RESTORE_SOURCE_URL" == "$TACIT_RESTORE_TARGET_URL" ]]; then
  echo "Restore source and target must differ." >&2
  exit 1
fi

psql_safe=(psql -X -v ON_ERROR_STOP=1 -At)
source_name="$("${psql_safe[@]}" "$TACIT_RESTORE_SOURCE_URL" -c 'select current_database()')"
target_name="$("${psql_safe[@]}" "$TACIT_RESTORE_TARGET_URL" -c 'select current_database()')"
if [[ "$source_name" != tacit_test_* || "$target_name" != tacit_test_* ]]; then
  echo "Restore verification requires tacit_test_ databases." >&2
  exit 1
fi

target_tables="$("${psql_safe[@]}" "$TACIT_RESTORE_TARGET_URL" -c "select count(*) from pg_tables where schemaname = 'public'")"
if [[ "$target_tables" != "0" ]]; then
  echo "Restore target must be empty." >&2
  exit 1
fi

dump_file="$(mktemp "${TMPDIR:-/tmp}/tacit-restore.XXXXXX")"
source_data="$(mktemp "${TMPDIR:-/tmp}/tacit-source.XXXXXX")"
target_data="$(mktemp "${TMPDIR:-/tmp}/tacit-target.XXXXXX")"
trap 'rm -f "$dump_file" "$source_data" "$target_data"' EXIT
chmod 600 "$dump_file" "$source_data" "$target_data"

pg_dump --format=custom --no-owner --no-privileges --file "$dump_file" "$TACIT_RESTORE_SOURCE_URL"
pg_restore --exit-on-error --no-owner --no-privileges --dbname "$TACIT_RESTORE_TARGET_URL" "$dump_file"

normalize_dump() {
  pg_dump --data-only --column-inserts --no-owner --no-privileges "$1" \
    | sed -e '/^--/d' -e '/^\\restrict /d' -e '/^\\unrestrict /d' -e '/^$/d' \
    | LC_ALL=C sort
}

normalize_dump "$TACIT_RESTORE_SOURCE_URL" > "$source_data"
normalize_dump "$TACIT_RESTORE_TARGET_URL" > "$target_data"
if ! cmp -s "$source_data" "$target_data"; then
  echo "Restored PostgreSQL data does not match the source." >&2
  exit 1
fi

source_version="$("${psql_safe[@]}" "$TACIT_RESTORE_SOURCE_URL" -c 'select max(version) from tacit_schema_migrations')"
target_version="$("${psql_safe[@]}" "$TACIT_RESTORE_TARGET_URL" -c 'select max(version) from tacit_schema_migrations')"
if [[ -z "$source_version" || "$source_version" != "$target_version" ]]; then
  echo "Restored schema version does not match the source." >&2
  exit 1
fi

echo "PostgreSQL restore verified at schema version $target_version."
