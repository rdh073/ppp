#!/usr/bin/env bash
set -euo pipefail

SERIAL=""
PACKAGE=""
ACTIVITY=""
COUNT=1
INTERVAL=1
OUTPUT_DIR="data/xml-dumps"
PREFIX="ui_dump"
REMOTE_PATH="/sdcard/window_dump.xml"

usage() {
  cat <<'EOF'
Dump Android UI hierarchy XML from APK/app using adb + uiautomator.

Usage:
  ./scripts/dump_ui_xml_from_apk.sh [options]

Options:
  --serial <adb-serial>      Target specific device/emulator serial.
  --package <package-name>   Launch package with monkey before dumping.
  --activity <activity>      Optional activity for am start (requires --package).
  --count <n>                Number of dumps to capture. Default: 1
  --interval <sec>           Sleep seconds between dumps. Default: 1
  --output-dir <dir>         Local output dir. Default: data/functiongemma/xml-dumps
  --prefix <name>            Output file prefix. Default: ui_dump
  --help                     Show this help.

Examples:
  ./scripts/dump_ui_xml_from_apk.sh --package com.example.app --count 10 --interval 2
  ./scripts/dump_ui_xml_from_apk.sh --serial emulator-5554 --package com.example.app --activity .MainActivity
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --serial)
      SERIAL="${2:-}"
      shift 2
      ;;
    --package)
      PACKAGE="${2:-}"
      shift 2
      ;;
    --activity)
      ACTIVITY="${2:-}"
      shift 2
      ;;
    --count)
      COUNT="${2:-}"
      shift 2
      ;;
    --interval)
      INTERVAL="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --prefix)
      PREFIX="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! command -v adb >/dev/null 2>&1; then
  echo "adb not found in PATH" >&2
  exit 1
fi

if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [[ "$COUNT" -lt 1 ]]; then
  echo "--count must be an integer >= 1" >&2
  exit 1
fi

if ! [[ "$INTERVAL" =~ ^[0-9]+$ ]] || [[ "$INTERVAL" -lt 0 ]]; then
  echo "--interval must be an integer >= 0" >&2
  exit 1
fi

if [[ -n "$ACTIVITY" && -z "$PACKAGE" ]]; then
  echo "--activity requires --package" >&2
  exit 1
fi

ADB=(adb)
if [[ -n "$SERIAL" ]]; then
  ADB+=( -s "$SERIAL" )
fi

if ! "${ADB[@]}" get-state >/dev/null 2>&1; then
  echo "No adb device available. Connect device/emulator first." >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

if [[ -n "$PACKAGE" ]]; then
  echo "Launching target app: $PACKAGE"
  if [[ -n "$ACTIVITY" ]]; then
    "${ADB[@]}" shell am start -n "${PACKAGE}/${ACTIVITY}" >/dev/null
  else
    "${ADB[@]}" shell monkey -p "$PACKAGE" -c android.intent.category.LAUNCHER 1 >/dev/null
  fi
  sleep 1
fi

timestamp="$(date +%Y%m%d_%H%M%S)"
echo "Capturing $COUNT XML dump(s) into: $OUTPUT_DIR"

for ((i=1; i<=COUNT; i++)); do
  seq_no="$(printf "%03d" "$i")"
  out_file="${OUTPUT_DIR}/${PREFIX}_${timestamp}_${seq_no}.xml"

  "${ADB[@]}" shell uiautomator dump "$REMOTE_PATH" >/dev/null
  "${ADB[@]}" pull "$REMOTE_PATH" "$out_file" >/dev/null
  echo "Saved: $out_file"

  if [[ "$i" -lt "$COUNT" && "$INTERVAL" -gt 0 ]]; then
    sleep "$INTERVAL"
  fi
done

echo "Done."
