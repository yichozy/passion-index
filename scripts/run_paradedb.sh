#!/bin/bash
# Run ParadeDB locally via Docker.
# PG 18+ images place data in /var/lib/postgresql/<major>/docker/, so we
# mount the parent dir — not /var/lib/postgresql/data.
#
# Prerequisite: free up port 5432 on the host (e.g., stop any local
# Postgres install). On Mac with homebrew postgres running, stop it via:
#   pg_ctl -D /opt/homebrew/var/postgresql@16 stop -m fast
set -eu

docker run -d --name paradedb \
	-e POSTGRES_PASSWORD=paradedb \
	-v paradedb-data:/var/lib/postgresql \
	-p 5432:5432 \
	paradedb/paradedb:0.25.2

echo
echo "connect:"
echo "  psql 'postgresql://postgres:paradedb@localhost:5432/postgres'"
echo "  docker exec -it paradedb psql -U postgres"
