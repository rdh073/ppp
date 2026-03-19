#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="${1:-./config/server.toml}"

cd "$ROOT_DIR/app/server-agent"
exec go run ./cmd/server -config "$CONFIG_PATH"
