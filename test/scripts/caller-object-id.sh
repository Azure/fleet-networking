#!/bin/bash
# Prints the object ID of the principal the Azure CLI is signed in as, read from its ARM access token,
# so that role assignments to the E2E test principal follow the service connection that runs the job.
set -o errexit
set -o nounset
set -o pipefail

token="$(az account get-access-token --query accessToken -o tsv)"
jq -Rr 'split(".")[1] | gsub("-"; "+") | gsub("_"; "/") | (. + "===") | .[0:(length - (length % 4))] |
  @base64d | fromjson | .oid' <<<"$token"
