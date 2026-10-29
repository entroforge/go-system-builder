package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestNativeS5IssuedViewDeliversRegisteredMethods(t *testing.T) {
	f := newStageAnswerFixture(t)
	for _, packet := range f.packets {
		var names []string
		for _, method := range packet.Methods {
			path := filepath.Join(f.root, filepath.FromSlash(method.Path))
			if _, err := os.ReadFile(path); err != nil {
				t.Fatal(err)
			}
			names = append(names, filepath.Base(filepath.Dir(method.Path)))
		}
		for _, required := range agentcontext.RequiredSkills(packet.Binding.Responsibility) {
			found := false
			for _, name := range names {
				found = found || name == required
			}
			if !found {
				t.Fatalf("%s view omitted %s: %v", packet.Binding.Responsibility, required, names)
			}
		}
	}
	// Re-observation preserves frozen references and original identities.
	packets, err := runtime.S5Packets(f.root, stageSnapshot(t, f.root).State, "")
	if err != nil || !reflect.DeepEqual(packets, f.packets) {
		t.Fatal("issued method projection changed on reuse", err)
	}
}

func TestNativeS6BriefDeliversOriginalMethodsAndConditionalRole(t *testing.T) {
	root, _, record := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(record)
	data, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), filepath.Join(root, record.AgentDefinition.Ref)) {
		t.Fatal("brief omitted actual role specialty conditions")
	}
	delivered := 0
	registered := make(map[string]bool, len(record.SkillRefs))
	for _, name := range record.SkillRefs {
		registered[name] = true
	}
	for _, source := range record.ContextSources {
		if source.Kind == "authorized_skill" && registered[source.ID] {
			if !strings.Contains(string(data), filepath.Join(root, source.Ref)) {
				t.Fatalf("brief omitted registered method %s", source.Ref)
			}
			delivered++
		} else if source.Kind == "authorized_skill" && strings.Contains(string(data), filepath.Join(root, source.Ref)) {
			t.Fatalf("brief imposed an unselected legacy method: %s", source.Ref)
		}
	}
	if delivered == 0 || delivered != len(record.SkillRefs) {
		t.Fatalf("brief source set differs from original registered skills: %d vs %d", delivered, len(record.SkillRefs))
	}
}
