package schema

import (
	"strings"
	"testing"
)

func TestEmbeddedAssetsIncludeCurrentAndExcludeHistoricalTemplates(t *testing.T) {
	for _, name := range []string{
		"stage-form-s2-template.json", "stage-form-s3-template.json", "stage-form-s4-template.json",
		"stage-form-s5-template.json", "stage-form-s7-template.json", "stage-form-s8-template.json",
		"stage-form-s9-plan-template.json", "stage-form-s9-result-template.json",
		"agent-message.schema.json", "activation.template.json", "loop-state.example.json",
		"agent-message.examples.json",
	} {
		if !AssetExists(name) {
			t.Errorf("current runtime asset %q is absent", name)
		}
	}
	for _, name := range AssetNames() {
		if strings.Contains(name, "-before-") || strings.Contains(name, "-legacy") {
			t.Errorf("historical template %q is embedded in the runtime", name)
		}
	}
}
