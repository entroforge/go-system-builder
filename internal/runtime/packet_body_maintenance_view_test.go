package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func maintenanceViewTestPackets(t *testing.T) []struct {
	name   string
	packet any
} {
	t.Helper()
	s7 := checkpointTestReviewPacket(t, t.TempDir())
	s9 := newRepairResultRuntimeFixture(t)
	return []struct {
		name   string
		packet any
	}{{"s7", s7}, {"s9-plan-report", s9.plan.packet}, {"s9-result", s9.packet}}
}

func maintenanceViewTestObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPacketMaintenanceViewsKeepCanonicalReadRootThroughAlias(t *testing.T) {
	fixture := newRepairResultRuntimeFixture(t)
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "main-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	packet := fixture.plan.packet
	display, err := originalPacketMaintenanceView(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	row := maintenanceViewTestObject(t, packet)
	path := filepath.Join(root, packet.ViewPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := maintainOriginalPacketView(context.Background(), alias, row, packet); err != nil {
		t.Fatalf("root alias changed original Read paths: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("root alias rewrote original view: %v", err)
	}
}

func TestPacketMaintenanceViewsPreserveEquivalentFormattingAndPrivateAnswers(t *testing.T) {
	for _, fixture := range maintenanceViewTestPackets(t) {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			row := maintenanceViewTestObject(t, fixture.packet)
			display, err := originalPacketMaintenanceView(root, fixture.packet)
			if err != nil {
				t.Fatal(err)
			}
			viewPath := filepath.Join(root, stringValue(row["view_path"]))
			answerPath := filepath.Join(root, stringValue(row["answer_path"]))
			for _, path := range []string{viewPath, answerPath} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			private := []byte("private author bytes are not an issued view\n")
			if err := os.WriteFile(answerPath, private, 0600); err != nil {
				t.Fatal(err)
			}
			for _, view := range []any{display, fixture.packet} {
				// A map changes object field order; tab indentation changes the
				// representation while preserving the exact original content.
				formatted, err := json.MarshalIndent(maintenanceViewTestObject(t, view), "", "\t")
				if err != nil {
					t.Fatal(err)
				}
				formatted = append(formatted, '\n', '\n')
				if err := os.WriteFile(viewPath, formatted, 0600); err != nil {
					t.Fatal(err)
				}
				if err := maintainOriginalPacketView(context.Background(), root, row, fixture.packet); err != nil {
					t.Fatalf("equivalent view rejected: %v", err)
				}
				if got, err := os.ReadFile(viewPath); err != nil || !bytes.Equal(got, formatted) {
					t.Fatalf("original display bytes changed: %v", err)
				}
			}
			if err := os.Remove(viewPath); err != nil {
				t.Fatal(err)
			}
			if err := maintainOriginalPacketView(context.Background(), root, row, fixture.packet); err != nil {
				t.Fatal(err)
			}
			expected, err := json.MarshalIndent(display, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(viewPath); err != nil || !bytes.Equal(got, append(expected, '\n')) {
				t.Fatalf("missing view did not recover original subtype projection: %v", err)
			}
			if got, err := os.ReadFile(answerPath); err != nil || !bytes.Equal(got, private) {
				t.Fatalf("private answer changed: %v", err)
			}
		})
	}
}

func TestPacketMaintenanceViewsRejectChangedQuestionsDefaultsReadRootsAndExtraFields(t *testing.T) {
	for _, fixture := range maintenanceViewTestPackets(t) {
		for _, change := range []string{"question", "default-answer", "read-root", "extra-field", "duplicate-key"} {
			t.Run(fixture.name+"/"+change, func(t *testing.T) {
				root := t.TempDir()
				row := maintenanceViewTestObject(t, fixture.packet)
				display, err := originalPacketMaintenanceView(root, fixture.packet)
				if err != nil {
					t.Fatal(err)
				}
				object := maintenanceViewTestObject(t, display)
				draft := object
				if nested, ok := object["draft"].(map[string]any); ok {
					draft = nested
				}
				switch change {
				case "question":
					questions := draft["questions"].(map[string]any)
					for _, raw := range questions {
						raw.(map[string]any)["question"] = "changed professional question"
						break
					}
				case "default-answer":
					draft["answer"].(map[string]any)["injected-author-answer"] = "changed default"
				case "read-root":
					draft["read"].([]any)[0].(map[string]any)["path"] = filepath.Join(t.TempDir(), "wrong-original-root")
				case "extra-field":
					object["unexpected-field"] = true
				}
				data, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				if change == "duplicate-key" {
					key := "title"
					if _, full := object["packet_id"]; full {
						key = "packet_id"
					}
					value, _ := json.Marshal(object[key])
					prefix := []byte("{\"" + key + "\":" + string(value) + ",")
					data = append(prefix, data[1:]...)
				}
				viewPath := filepath.Join(root, stringValue(row["view_path"]))
				answerPath := filepath.Join(root, stringValue(row["answer_path"]))
				for _, path := range []string{viewPath, answerPath} {
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
				}
				private := []byte("private professional draft remains unchanged")
				if err := os.WriteFile(viewPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(answerPath, private, 0600); err != nil {
					t.Fatal(err)
				}
				if err := maintainOriginalPacketView(context.Background(), root, row, fixture.packet); err == nil {
					t.Fatal("changed or malformed issued view accepted")
				}
				if got, err := os.ReadFile(viewPath); err != nil || !bytes.Equal(got, data) {
					t.Fatalf("rejected view overwritten: %v", err)
				}
				if got, err := os.ReadFile(answerPath); err != nil || !bytes.Equal(got, private) {
					t.Fatalf("private answer overwritten: %v", err)
				}
			})
		}
	}
}
