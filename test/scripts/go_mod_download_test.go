/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDockerGoModDownloadRetry(t *testing.T) {
	tests := []struct {
		failures int
		calls    int
		delays   string
		wantErr  bool
	}{
		{0, 1, "", false},
		{1, 2, "10\n", false},
		{2, 3, "10\n20\n", false},
		{3, 3, "10\n20\n", true},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.failures), func(t *testing.T) {
			dir := t.TempDir()
			writeAFDCommand(t, filepath.Join(dir, "go"), `#!/bin/sh
[ "$*" = "mod download" ] || exit 99
echo attempt >> "$TEST_ATTEMPTS"
attempts=$(wc -l < "$TEST_ATTEMPTS")
[ "$attempts" -gt "$TEST_FAILURES" ]
`)
			writeAFDCommand(t, filepath.Join(dir, "sleep"), "#!/bin/sh\necho \"$1\" >> \"$TEST_DELAYS\"\n")
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TEST_ATTEMPTS", filepath.Join(dir, "attempts"))
			t.Setenv("TEST_DELAYS", filepath.Join(dir, "delays"))
			t.Setenv("TEST_FAILURES", strconv.Itoa(tt.failures))

			output, err := exec.Command("sh", "../../hack/retry-go-mod-download.sh").CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected result: %v, output: %s", err, output)
			}
			attempts, err := os.ReadFile(filepath.Join(dir, "attempts"))
			if err != nil || strings.Count(string(attempts), "attempt\n") != tt.calls {
				t.Fatalf("unexpected attempts %q: %v", attempts, err)
			}
			delays, err := os.ReadFile(filepath.Join(dir, "delays"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(delays) != tt.delays {
				t.Fatalf("got delays %q, want %q", delays, tt.delays)
			}
			if tt.wantErr && !strings.Contains(string(output), "failed after 3 attempts") {
				t.Fatalf("missing exhaustion error: %s", output)
			}
		})
	}
}
