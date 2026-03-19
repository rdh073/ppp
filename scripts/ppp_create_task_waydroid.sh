#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <DEVICE_ID> [BASE_URL]"
  exit 1
fi

DEVICE_ID="$1"
BASE_URL="${2:-http://localhost:3000}"

curl -sS -X POST "$BASE_URL/tasks" \
  -H 'Content-Type: application/json' \
  -d "{
    \"goal\":\"create google account on waydroid\",
    \"deviceId\":\"$DEVICE_ID\",
    \"workflowName\":\"google-account-create-waydroid\"
  }"
echo
