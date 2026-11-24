#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set +x

: "${AZURE_SUBSCRIPTION_ID:?AZURE_SUBSCRIPTION_ID is required}"

az account get-access-token --subscription "$AZURE_SUBSCRIPTION_ID" \
  --resource https://management.azure.com/ --query accessToken -o tsv |
  python3 -c '
import base64
import binascii
import json
import re
import sys

try:
    parts = sys.stdin.read().strip().split(".")
    if len(parts) != 3:
        raise ValueError("invalid token")
    payload = parts[1] + "=" * (-len(parts[1]) % 4)
    principal = json.loads(base64.urlsafe_b64decode(payload))["oid"]
    if not isinstance(principal, str) or not re.fullmatch(
        r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}", principal
    ):
        raise ValueError("invalid object ID")
except (binascii.Error, ValueError, UnicodeError, KeyError, TypeError):
    sys.exit("error: Azure CLI token does not contain a valid principal object ID")
print(principal.lower())
'
