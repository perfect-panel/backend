#!/usr/bin/env bash
# golang-migrate only applies versions above the last recorded version.
# Keep the stable baseline intact and add new numbers above its highest one.
set -euo pipefail

readonly base="${MIGRATION_BASE:-origin/main}"
readonly MIGRATION_DIR=internal/app/migration/schema/database
readonly LEGACY_MIGRATION_DIR=initialize/migrate/database

extract_numbers() {
  sed -n 's#.*/\([0-9]\{5\}\)_.*\.up\.sql$#\1#p' | sort -u
}

ref_migration_numbers() {
  # A branch may still predate the package reorganization. Directory moves do
  # not create new migrations, so compare the same versions at either path.
  git ls-tree -r --name-only "$1" -- "$MIGRATION_DIR/$2" "$LEGACY_MIGRATION_DIR/$2" | extract_numbers
}

# The working tree rather than HEAD, so an uncommitted migration is caught too.
# In CI the two are identical.
local_migration_numbers() {
  find "$MIGRATION_DIR/$1" -name '*.up.sql' | extract_numbers
}

if ! git rev-parse --verify --quiet "${base}^{commit}" >/dev/null; then
  echo "Migration baseline $base is unavailable. Fetch main or set MIGRATION_BASE." >&2
  exit 1
fi

status=0
mysql_numbers="$(local_migration_numbers mysql)"
postgres_numbers="$(local_migration_numbers postgres)"
asymmetric="$(comm -3 <(echo "$mysql_numbers") <(echo "$postgres_numbers") | tr -d '\t')"
if [ -n "$asymmetric" ]; then
  echo "These migrations are missing one of the two dialects:"
  printf '%s\n' "$asymmetric"
  status=1
fi

base_numbers="$(ref_migration_numbers "$base" mysql)"
missing="$(comm -23 <(echo "$base_numbers") <(echo "$mysql_numbers"))"
if [ -n "$missing" ]; then
  echo "These migrations exist on $base but not here:"
  printf '%s\n' "$missing"
  status=1
fi

highest="$(tail -n1 <<< "$base_numbers")"
while read -r number; do
  if [[ -n $number && -n $highest && $number < $highest ]]; then
    echo "New migration $number falls below $highest on $base and would be skipped."
    status=1
  fi
done < <(comm -13 <(echo "$base_numbers") <(echo "$mysql_numbers"))

exit "$status"
