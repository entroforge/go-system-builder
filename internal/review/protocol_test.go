package review

import "testing"

func TestReviewProtocolCannotCrossTheWriterFence(t *testing.T) {
	for _, runtimeVersion := range []string{"1.1.0", "2.0.0", "9.0.0"} {
		for _, artifactVersion := range []string{"1.0.0", "2.0.0", "9.0.0"} {
			state := map[string]any{"schema_version": runtimeVersion}
			err := ValidateProtocolBinding(state, artifactVersion)
			want := runtimeVersion == "1.1.0" && artifactVersion == "1.0.0"
			if (err == nil) != want {
				t.Fatalf("Runtime=%s artifact=%s: %v", runtimeVersion, artifactVersion, err)
			}
		}
	}
}
