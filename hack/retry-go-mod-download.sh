#!/bin/sh
set -eu

for attempt in 1 2 3; do
  if go mod download; then
    exit 0
  fi
  if [ "$attempt" -eq 3 ]; then
    echo "error: go mod download failed after 3 attempts" >&2
    exit 1
  fi
  delay=$((attempt * 10))
  echo "go mod download failed; retrying in $delay seconds..." >&2
  sleep "$delay"
done
