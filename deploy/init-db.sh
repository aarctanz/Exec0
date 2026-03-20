#!/bin/bash
set -e

# Run all up migrations in order against the database
for f in /migrations/*.up.sql; do
    echo "Applying migration: $(basename "$f")"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -f "$f"
done

echo "All migrations applied."
