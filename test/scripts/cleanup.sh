#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

case "${RETAIN_AFD_E2E_SETUP:-false}" in
  true)
    echo "error: AFD setup is retained; clear RETAIN_AFD_E2E_SETUP only when explicit cleanup is authorized" >&2
    exit 1
    ;;
  false) ;;
  *)
    echo "error: RETAIN_AFD_E2E_SETUP must be true or false" >&2
    exit 1
    ;;
esac

exec bash "$(dirname "${BASH_SOURCE[0]}")/cleanup-shared-group.sh"
