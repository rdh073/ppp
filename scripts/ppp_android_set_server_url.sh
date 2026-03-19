#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <WS_SERVER_URL>"
  echo "Example: $0 ws://10.0.2.2:3000/ws/agent"
  exit 1
fi

WS_SERVER_URL="$1"
adb shell setprop auto.agent.server_url "$WS_SERVER_URL"
echo "Set auto.agent.server_url=$WS_SERVER_URL"
