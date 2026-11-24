/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAzurePrincipalID(t *testing.T) {
	const id = "01234567-89ab-cdef-0123-456789abcdef"
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"oid":"` + id + `"}`))
	for _, tt := range []struct {
		name, token string
		fails       bool
	}{
		{"valid", "header." + payload + ".signature", false},
		{"malformed", "not-a-token", true},
		{"missing object ID", "header.e30.signature", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			dir := t.TempDir()
			writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
set -euo pipefail
[[ "$*" == "account get-access-token --subscription test-subscription --resource https://management.azure.com/ --query accessToken -o tsv" ]]
printf '%s\n' "$TEST_TOKEN"
`)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TEST_TOKEN", tt.token)
			output, err := runAFDScript(t, "-x", "./azure-principal-id.sh")
			if (err != nil) != tt.fails || strings.Contains(output, tt.token) {
				t.Fatalf("principal extraction: %s (%v)", output, err)
			}
			if !tt.fails && !strings.Contains(output, id) {
				t.Fatalf("principal ID missing: %s", output)
			}
			if tt.fails && !strings.Contains(output, "error:") {
				t.Fatalf("invalid token was not reported: %s", output)
			}
		})
	}
}
