#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# shellcheck source=test/scripts/e2e-config.sh
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"

if [[ "$ENABLE_AZURE_FRONT_DOOR" == true ]]; then
  exec go test -count=1 -timeout 10h -tags=e2e -v ./test/e2e -args -ginkgo.v -ginkgo.timeout=9h30m
fi
exec go test -timeout 50m -tags=e2e -v ./test/e2e -args -ginkgo.v
