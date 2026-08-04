package transition_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestManualHeaderContainsNoVolatileTimestamp(t *testing.T) {
	manual := transition.RenderManual(&transition.LoopDefinition{}, transition.ManualOptions{
		TargetPath:           "loop-harness.md",
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: strings.Repeat("a", 64),
	})
	if strings.Contains(manual, "**Generated**") {
		t.Fatalf("manual output must be deterministic; got volatile timestamp header: %s", manual)
	}
}
