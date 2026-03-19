#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://localhost:3000}"

has_jq() {
  command -v jq >/dev/null 2>&1
}

echo "[healthz]"
curl -sS "$BASE_URL/healthz"
echo

echo "[devices]"
if has_jq; then
  curl -sS "$BASE_URL/devices" | jq
else
  curl -sS "$BASE_URL/devices"
  echo
fi

echo "[workflows]"
if has_jq; then
  curl -sS "$BASE_URL/workflows" | jq '.[].name'
else
  curl -sS "$BASE_URL/workflows"
  echo
fi
