package evidence

import "testing"

func TestLocalCaptureSourceSeparatesCredentialsFromObservations(t *testing.T) {
	for _, path := range []string{".env", "config/.env.production", ".aws/credentials", ".ssh/id_ed25519", ".git/config", ".kube/config", "config/credentials.json", "tls/client.p12", "home/.netrc"} {
		if err := ValidateLocalCaptureSource(path); err == nil {
			t.Errorf("credential capture allowed: %s", path)
		}
	}
	for _, path := range []string{"logs/redacted.txt", ".claude/evidence/s8/capture", ".env.example", "config/.env.production.template", "docs/credentials-guide.md", "keys/id_ed25519.pub"} {
		if err := ValidateLocalCaptureSource(path); err != nil {
			t.Errorf("ordinary observation rejected: %s: %v", path, err)
		}
	}
	// New capture policy does not reinterpret or invalidate a historical ref.
	if _, _, err := LocalPathReference("path:.env"); err != nil {
		t.Fatalf("historical path parsing changed: %v", err)
	}
}
