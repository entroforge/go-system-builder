package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSurfaceVersionChecksActualDeclaredSources(t *testing.T) {
	for _, tc := range []struct {
		name, profile, reference, headerID, headerVersion, inherits, kernelVersion, want string
	}{
		{"matching", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.3.4", "SUR-01", "v2.3.4", "DESIGN.md@v1.0.0", "v1.0.0", ""},
		{"stale derivation", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.0.0", "SUR-01", "v2.3.4", "DESIGN.md@v1.0.0", "v1.0.0", "surface_version_mismatch"},
		{"stale table pin", "surface-profiles/consumer.md@v2.0.0", "SUR-01@v2.3.4", "SUR-01", "v2.3.4", "DESIGN.md@v1.0.0", "v1.0.0", "surface_version_mismatch"},
		{"stale inheritance", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.3.4", "SUR-01", "v2.3.4", "DESIGN.md@v0.9.0", "v1.0.0", "surface_version_mismatch"},
		{"no duplicate table version", "surface-profiles/consumer.md", "SUR-01@v2.3.4", "SUR-01", "v2.3.4", "DESIGN.md@v1.0.0", "v1.0.0", ""},
		{"inline", "inline", "SUR-01@v1.0.0", "", "", "", "v1.0.0", ""},
		{"inline without extra surface pin", "inline", "SUR-01", "", "", "", "v1.0.0", ""},
		{"stale inline", "inline", "SUR-01@v0.9.0", "", "", "", "v1.0.0", "surface_version_mismatch"},
		{"unknown surface", "inline", "SUR-99@v1.0.0", "", "", "", "v1.0.0", "surface_version_unverified"},
		{"unknown source version", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.3.4", "SUR-01", "vX.Y.Z", "", "v1.0.0", "surface_version_unverified"},
		{"unknown reference version", "surface-profiles/consumer.md@v2.3.4", "SUR-01@vX.Y.Z", "SUR-01", "v2.3.4", "", "v1.0.0", "surface_version_unverified"},
		{"wrong source identity", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.3.4", "SUR-02", "v2.3.4", "", "v1.0.0", "surface_version_unverified"},
		{"missing profile", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.3.4", "", "", "", "v1.0.0", "surface_version_unverified"},
		{"kernel unknown must not hide surface mismatch", "surface-profiles/consumer.md@v2.3.4", "SUR-01@v2.0.0", "SUR-01", "v2.3.4", "", "vX.Y.Z", "surface_version_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			kernel := strings.Replace(kernelWithVersion(tc.kernelVersion), "surface-profiles/consumer.md@v1.0.0", tc.profile, 1)
			files := map[string]string{
				KernelRel:                           kernel,
				TokensJSONRel:                       "{}",
				"docs/design/derivation/REQ-731.md": "# Derivation\n\n> Foundation：docs/design/DESIGN.md@" + tc.kernelVersion + "\n> Surface：" + tc.reference + "\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|---|---|---|\n| LAW-01 | reading | GR-01 |\n",
			}
			if tc.headerID != "" {
				files["docs/design/surface-profiles/consumer.md"] = "# Surface\n\n> ID：" + tc.headerID + "\n> 版本：" + tc.headerVersion + "\n> 继承：" + tc.inherits + "\n"
			}
			mustWriteTreeGeneric(t, root, files)
			report, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			var versions []Finding
			for _, f := range report.Findings {
				if f.Code == "surface_version_mismatch" || f.Code == "surface_version_unverified" {
					versions = append(versions, f)
				}
			}
			if tc.want == "" && len(versions) > 0 {
				t.Fatalf("valid original source acquired version work: %+v", versions)
			}
			if tc.want != "" {
				found := false
				for _, f := range versions {
					if f.Code == tc.want && f.Path == "docs/design/derivation/REQ-731.md" && strings.Contains(f.Detail, "SUR-") {
						found = true
					}
				}
				if !found {
					t.Errorf("actual surface chain not checked: want %s, got %+v", tc.want, versions)
				}
			}
			for rel, want := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil || string(data) != want {
					t.Fatalf("advisory check changed source %s: %v", rel, err)
				}
			}
			if tc.profile == "inline" {
				if _, err := os.Stat(filepath.Join(root, "docs/design/surface-profiles")); !os.IsNotExist(err) {
					t.Fatal("inline surface required an extra profile")
				}
			}
		})
	}
}
