#!/usr/bin/env bash
# Print the local URLs of the running stack (used by `make dev`). Host ports
# are read from the running containers, so .env overrides are reflected.
# Usage: scripts/dev-urls.sh [docker compose global flags]
set -euo pipefail

compose=(docker compose "$@")

# host:port that a service's container port is published on, or empty.
port_of() {
	"${compose[@]}" port "$1" "$2" 2>/dev/null | head -n1 | sed 's/^0\.0\.0\.0:/localhost:/; s/^127\.0\.0\.1:/localhost:/'
}

frontend=$(port_of frontend 8080)
backend=$(port_of backend 8080)
mongo_ui=$(port_of mongo-express 8081)
console=$(port_of rustfs 9001)
mongodb=$(port_of mongodb 27017)

cat <<EOF

Elimin8 local stack is up:

  App (frontend)       http://${frontend:-?}
  Backend readiness    http://${backend:-?}/api/health/ready
  MongoDB UI           http://${mongo_ui:-?}   (login: MONGO_UI_USERNAME / MONGO_UI_PASSWORD in .env)
  RustFS console       http://${console:-?}/rustfs/console/   (login: PHOTO_STORAGE_ACCESS_KEY / PHOTO_STORAGE_SECRET_KEY in .env)
  MongoDB              mongodb://${mongodb:-?}/elimin8

No sample data is seeded yet (planned in #96/#97).
EOF
