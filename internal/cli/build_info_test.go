package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

func TestBuildInfoDoesNotNeedProject(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"version"}, nil, &out, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, stderr.String())
	}
	var info map[string]any
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["os"] != runtime.GOOS || info["arch"] != runtime.GOARCH || info["version"] == "" {
		t.Fatalf("%v", info)
	}
}
