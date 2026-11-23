#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ ! -f internal/server/webdist/dist/index.html ]]; then
  ./scripts/build-web.sh
fi

# Keep the local development database in sync before starting the API. The
# migration command is idempotent and avoids serving handlers against a stale
# schema after pulling new backend code.
go run ./cmd/claw-gateway migrate up

exec go run ./cmd/claw-gateway "$@"
