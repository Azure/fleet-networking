#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
  echo "error: provide a Go command to retry" >&2
  exit 1
fi

for attempt in 1 2 3; do
  if go "$@"; then
    exit 0
  fi
  if [ "$attempt" -eq 3 ]; then
    echo "error: go $* failed after 3 attempts" >&2
    exit 1
  fi
  delay=$((attempt * 10))
  echo "go $* failed; retrying in $delay seconds..." >&2
  sleep "$delay"
done
