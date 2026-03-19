#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <DEVICE_ID> [PRIVATE_DNS_HOSTNAME] [BASE_URL]"
  exit 1
fi

DEVICE_ID="$1"
PRIVATE_DNS_HOSTNAME="${2:-dns.quad9.net}"
BASE_URL="${3:-http://localhost:3000}"

curl -sS -X POST "$BASE_URL/tasks" \
  -H 'Content-Type: application/json' \
  -d "{
    \"goal\":\"set private dns\",
    \"deviceId\":\"$DEVICE_ID\",
    \"workflowName\":\"android-settings-private-dns\",
    \"inputArtifacts\":{
      \"private_dns_hostname\":\"$PRIVATE_DNS_HOSTNAME\"
    }
  }"
echo
