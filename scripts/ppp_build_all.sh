#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[1/3] Build server-agent"
(
  cd "$ROOT_DIR/app/server-agent"
  go mod tidy
  go build -o bin/server-agent ./cmd/server
  go test ./...
)

echo "[2/3] Build android-agent"
(
  cd "$ROOT_DIR/app/android-agent"
  ./gradlew assembleDebug
  ./gradlew test
)

echo "[3/3] Build dashboard"
(
  cd "$ROOT_DIR/app/dashboard"
  npm install
  npm run build
)

echo "Done."
