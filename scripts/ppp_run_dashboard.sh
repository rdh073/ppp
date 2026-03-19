#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

API_URL="${VITE_API_URL:-http://localhost:3000}"
POLL_MS="${VITE_POLL_MS:-5000}"
HOST="${1:-0.0.0.0}"
PORT="${2:-5173}"

cd "$ROOT_DIR/app/dashboard"
export VITE_API_URL="$API_URL"
export VITE_POLL_MS="$POLL_MS"
exec npm run dev -- --host "$HOST" --port "$PORT"
