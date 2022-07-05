package uniquename

import (
	"regexp"
	"strings"
	"testing"
)

const (
	clusterID                   = "bravelion"
	objectNS                    = "work"
	objectName                  = "app"
	expectedClusterScopedPrefix = "work-app-"
	expectedClusterScopedLength = 14
	expectedFleetScopedPrefix   = "bravelion-work-app-"
	expectedFleetScopedLength   = 24

	longClusterID  = "lhwh0nrw3d03no2f1rf53dl9gliwe3rneqeuzt6k2qa9qn6epm"           // 50 characters long
	longObjectNS   = "mdlmpqe2ev31zgxar1gswscd3hsvypl5leh4nolfnq0vdg56a7a08iiu4988" // 60 characters long
	longObjectName = "c7t2c6oppjnryqcihwweexeobs7tlmf08ha4qb5htc4cifzpalhb5ec2lbh3" +
		"j73reciaz2f0jfd2rl5qba6rzuuwgyw6d9e6la19bo89k41lphln4s4dy1gr" +
		"h1dvua17iu4ro61dxo91ayovns8cgnmshlsflmi68e3najm7dw5dqe17pih7" +
		"up0dtyvrqxyp90sxedbf" // 200 characters long
)

// TestClusterScopedUniqueName tests the ClusterScopedUniqueName function.
func TestClusterScopedUniqueName(t *testing.T) {
	testCases := []struct {
		name       string
		format     UniqueNameFormat
		objectNS   string
		objectName string
		wantPrefix string
		wantLength int
	}{
		{
			name:       "should format RFC 1123 DNS subdomain name",
			format:     DNS1123Subdomain,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS subdomain name (truncated)",
			format:     DNS1123Subdomain,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "mdlmpqe2ev31zgxar1gswscd3hsvypl5leh4nolfnq0vdg56a7a08iiu4988" +
				"-c7t2c6oppjnryqcihwweexeobs7tlmf08ha4qb5htc4cifzpalhb5ec2lbh" +
				"3j73reciaz2f0jfd2rl5qba6rzuuwgyw6d9e6la19bo89k41lphln4s4dy1g" +
				"rh1d-", // 184 characters long
			wantLength: 190,
		},
		{
			name:       "should format RFC 1123 DNS label name",
			format:     DNS1123Label,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS label name (no dots allowed)",
			format:     DNS1123Label,
			objectNS:   objectNS,
			objectName: objectName + ".",
			wantPrefix: expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS label name (truncated)",
			format:     DNS1123Label,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "mdlmpqe2ev31zgxar1gswscd3hsv-c7t2c6oppjnryqcihwweexeobs7t-",
			wantLength: 63,
		},
		{
			name:       "should format RFC 1035 DNS label name",
			format:     DNS1035Label,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength,
		},
		{
			name:       "should format RFC 1035 DNS label name (no dots allowed)",
			format:     DNS1035Label,
			objectNS:   objectNS,
			objectName: objectName + ".",
			wantPrefix: expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength,
		},
		{
			name:       "should format RFC 1035 DNS label name (no numeric starts allowed)",
			format:     DNS1035Label,
			objectNS:   "0" + objectNS,
			objectName: objectName,
			wantPrefix: "ns0" + expectedClusterScopedPrefix,
			wantLength: expectedClusterScopedLength + 3,
		},
		{
			name:       "should format RFC 1035 DNS label name (truncated)",
			format:     DNS1035Label,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "mdlmpqe2ev31zgxar1gswscd3hsv-c7t2c6oppjnryqcihwweexeobs7t-",
			wantLength: 63,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uniqueName, err := ClusterScopedUniqueName(tc.format, tc.objectNS, tc.objectName)
			if err != nil {
				t.Fatalf("ClusterScopedUniqueName(%d, %s, %s), got %v, want no error", tc.format, tc.objectNS, tc.objectName, err)
			}
			if !strings.HasPrefix(uniqueName, tc.wantPrefix) {
				t.Errorf("ClusterScopedUniqueName(%d, %s, %s)=%s, want prefix %s",
					tc.format, tc.objectNS, tc.objectName, uniqueName, tc.wantPrefix)
			}
			if len(uniqueName) != tc.wantLength {
				t.Errorf("ClusterScopedUniqueName(%d, %s, %s)=%s, got length %d, want length %d",
					tc.format, tc.objectNS, tc.objectName, uniqueName, len(uniqueName), tc.wantLength)
			}
		})
	}
}

// TestFleetScopedUniqueName tests the FleetScopedUniqueName function.
func TestFleetScopedUniqueName(t *testing.T) {
	testCases := []struct {
		name       string
		format     UniqueNameFormat
		clusterID  string
		objectNS   string
		objectName string
		wantPrefix string
		wantLength int
	}{
		{
			name:       "should format RFC 1123 DNS subdomain name",
			format:     DNS1123Subdomain,
			clusterID:  clusterID,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS subdomain name (truncated)",
			format:     DNS1123Subdomain,
			clusterID:  longClusterID,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "lhwh0nrw3d03no2f1rf53dl9gliwe3rneqeuzt6k2qa9qn6epm-mdlmpqe2e" +
				"v31zgxar1gswscd3hsvypl5leh4nolfnq0vdg56a7a08iiu4988-c7t2c6op" +
				"pjnryqcihwweexeobs7tlmf08ha4qb5htc4cifzpalhb5ec2lbh3j73recia" +
				"z2f0jfd2rl5qb-",
			wantLength: 199,
		},
		{
			name:       "should format RFC 1123 DNS label name",
			format:     DNS1123Label,
			clusterID:  clusterID,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS label name (truncated)",
			format:     DNS1123Label,
			clusterID:  longClusterID,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "lhwh0nrw3d03no2f1r-mdlmpqe2ev31zgxar1-c7t2c6oppjnryqcihw-",
			wantLength: 62,
		},
		{
			name:       "should format RFC 1123 DNS label name (no dots allowed in cluster ID)",
			format:     DNS1123Label,
			clusterID:  clusterID + ".",
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS label name (not dots allowed in name)",
			format:     DNS1123Label,
			clusterID:  clusterID,
			objectNS:   objectNS,
			objectName: objectName + ".",
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1035 DNS label name",
			format:     DNS1035Label,
			clusterID:  clusterID,
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1035 DNS label name (truncated)",
			format:     DNS1035Label,
			clusterID:  longClusterID,
			objectNS:   longObjectNS,
			objectName: longObjectName,
			wantPrefix: "lhwh0nrw3d03no2f1r-mdlmpqe2ev31zgxar1-c7t2c6oppjnryqcihw-",
			wantLength: 62,
		},
		{
			name:       "should format RFC 1123 DNS label name (no dots allowed in cluster ID)",
			format:     DNS1035Label,
			clusterID:  clusterID + ".",
			objectNS:   objectNS,
			objectName: objectName,
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
		{
			name:       "should format RFC 1123 DNS label name (not dots allowed in name)",
			format:     DNS1035Label,
			clusterID:  clusterID,
			objectNS:   objectNS,
			objectName: objectName + ".",
			wantPrefix: expectedFleetScopedPrefix,
			wantLength: expectedFleetScopedLength,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uniqueName, err := FleetScopedUniqueName(tc.format, tc.clusterID, tc.objectNS, tc.objectName)
			if err != nil {
				t.Fatalf("FleetScopedUniqueName(%d, %s, %s, %s), got %v, want no error", tc.format, tc.clusterID, tc.objectNS, tc.objectName, err)
			}
			if !strings.HasPrefix(uniqueName, tc.wantPrefix) {
				t.Errorf("FleetScopedUniqueName(%d, %s, %s, %s)=%s, want prefix %s",
					tc.format, tc.clusterID, tc.objectNS, tc.objectName, uniqueName, tc.wantPrefix)
			}
			if len(uniqueName) != tc.wantLength {
				t.Errorf("FleetScopedUniqueName(%d, %s, %s, %s)=%s, got length %d, want length %d",
					tc.format, tc.clusterID, tc.objectNS, tc.objectName, uniqueName, len(uniqueName), tc.wantLength)
			}
		})
	}
}

// TestRandomLowerCaseAlphabeticString tests the RandomLowerCaseAlphabeticString function.
func TestRandomLowerCaseAlphabeticString(t *testing.T) {
	testCases := []struct {
		name   string
		length int
	}{
		{
			name:   "should return lower case alphabetic string",
			length: 20,
		},
	}

	var isAlphabeticString = regexp.MustCompile(`^[a-z]+$`).MatchString

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := RandomLowerCaseAlphabeticString(tc.length)
			if len(s) != tc.length {
				t.Errorf("RandomLowerCaseAlphabeticString(%d)=%s, got length %d, want length %d", tc.length, s, len(s), tc.length)
			}
			if !isAlphabeticString(s) {
				t.Errorf("RandomLowerCaseAlphabeticString(%d)=%s, want lower case alphabetic characters only", tc.length, s)
			}
		})
	}
}
