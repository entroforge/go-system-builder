package e2erun

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func testEvents(t *testing.T, tests []Test, body ...map[string]any) string {
	t.Helper()
	events := []map[string]any{{"type": "begin", "tests": tests}}
	events = append(events, body...)
	var s strings.Builder
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		s.Write(data)
		s.WriteByte('\n')
	}
	return s.String()
}
func fixtureTest() Test {
	return Test{ID: "test-one", Path: "test/example.spec.js", Project: "firefox", Browser: "firefox", SelectionPath: "example.spec.js", TitlePath: []string{"save"}, ExpectedStatus: "passed", CaseRefs: []CaseRef{{ModuleRef: "docs/design/prototypes/settings/cases.json", CaseID: "CASE-001", OracleRef: "/oracle/visible/0", Persona: "owner", DataProfile: "default"}}}
}
func TestReceiptRequiresCompleteUniqueObservedExecution(t *testing.T) {
	test := fixtureTest()
	begin := map[string]any{"type": "test_begin", "test_id": test.ID, "retry": 0}
	pass := map[string]any{"type": "test_end", "test_id": test.ID, "retry": 0, "status": "passed", "duration_ms": 2, "errors": []string{}}
	end := map[string]any{"type": "end", "status": "passed"}
	for _, tc := range []struct {
		name     string
		tests    []Test
		events   []map[string]any
		complete bool
		outcome  string
	}{
		{"pass", []Test{test}, []map[string]any{begin, pass, end}, true, "pass"},
		{"comment_only", nil, []map[string]any{end}, false, "unknown"},
		{"duplicate_test_id", []Test{test, test}, []map[string]any{begin, pass, end}, false, "unknown"},
		{"missing_begin", []Test{test}, []map[string]any{pass, end}, false, "unknown"},
		{"duplicate_execution", []Test{test}, []map[string]any{begin, pass, pass, end}, false, "unknown"},
		{"missing_end", []Test{test}, []map[string]any{begin, pass}, false, "unknown"},
		{"unfinished", []Test{test}, []map[string]any{begin, end}, false, "unknown"},
		{"skipped", []Test{test}, []map[string]any{begin, {"type": "test_end", "test_id": test.ID, "status": "skipped"}, end}, true, "fail"},
		{"failed_retry_pass", []Test{test}, []map[string]any{begin, {"type": "test_end", "test_id": test.ID, "status": "failed"}, {"type": "test_begin", "test_id": test.ID, "retry": 1}, {"type": "test_end", "test_id": test.ID, "retry": 1, "status": "passed"}, end}, true, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Receipt{Mode: "execution", RunID: "run", Outcome: "unknown", Inputs: []FileDigest{{Path: test.Path}}, RawEvents: testEvents(t, tc.tests, tc.events...)}
			parseEvents(&r, map[string]bool{test.ID: true})
			if r.Complete != tc.complete || r.Outcome != tc.outcome {
				t.Fatalf("receipt=%+v", r)
			}
		})
	}
	r := Receipt{Mode: "execution", RunID: "run", Outcome: "unknown", Inputs: []FileDigest{{Path: test.Path}}, RawEvents: testEvents(t, []Test{test}, begin, pass, end)}
	parseEvents(&r, map[string]bool{"missing-shard-test": true})
	if r.Complete {
		t.Fatal("wrong shard became complete")
	}
	if _, ok := (&Observation{}).Receipt(); ok {
		t.Fatal("caller-created zero Observation is trusted")
	}
	var imported Observation
	if err := json.Unmarshal([]byte(`{"data":"forged","complete":true,"outcome":"pass"}`), &imported); err != nil {
		t.Fatal(err)
	}
	if _, ok := imported.Receipt(); ok {
		t.Fatal("handwritten JSON became a runner Observation")
	}
}

func TestSnapshotReadsDeclaredViewAndRejectsToolEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "test"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "test/input.js"), []byte("input"), 0644); err != nil {
		t.Fatal(err)
	}
	inputs, hash, err := snapshot(context.Background(), fileview.Disk{Root: root}, []string{"test"}, t.TempDir())
	if err != nil || len(inputs) != 1 || len(hash) != 64 {
		t.Fatalf("%v %v %s", err, inputs, hash)
	}
	if _, _, err := snapshot(context.Background(), fileview.Disk{Root: root}, []string{"../outside"}, t.TempDir()); err == nil {
		t.Fatal("escaping source accepted")
	}
	tools := t.TempDir()
	if err := os.Symlink(root, filepath.Join(tools, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := copyTool(context.Background(), Tool{Path: tools}, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("tool closure escaped")
	}
}

// This test runs real Playwright + a provisioned browser in a private network namespace.
// Tool paths are explicitly provided; missing capability is a failure when
// enabled. Normal unit tests do not download or install tools.
func TestControlledPlaywrightIntegration(t *testing.T) {
	if os.Getenv("GSB_E2E_INTEGRATION") != "1" {
		t.Skip("set GSB_E2E_INTEGRATION=1 and explicit provisioned tool paths")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "host service must stay unreachable") })}
	go server.Serve(listener)
	defer server.Close()
	browserName, browserExecutable := os.Getenv("GSB_E2E_BROWSER_NAME"), os.Getenv("GSB_E2E_BROWSER_EXECUTABLE")
	if browserName == "" {
		browserName = "firefox"
	}
	if browserExecutable == "" {
		browserExecutable = "firefox/firefox"
	}
	if _, err := relative(browserExecutable); err != nil {
		t.Fatal(err)
	}
	nameJSON, _ := json.Marshal(browserName)
	executableJSON, _ := json.Marshal("/opt/browsers/" + browserExecutable)
	config := fmt.Sprintf(`module.exports={testDir:'.',use:{browserName:%s,launchOptions:{executablePath:%s}}};`, nameJSON, executableJSON)
	spec := fmt.Sprintf(`const {test,expect}=require('playwright/test');
const fs=require('fs');
test('settings save', {annotation:{type:'gsb.case',description:JSON.stringify({module_ref:'docs/design/prototypes/settings/cases.json',case_id:'CASE-001',oracle_ref:'/oracle/visible/0',persona:'owner',data_profile:'default'})}},async({page})=>{
 expect(fs.existsSync('/usr/bin/python3')).toBeFalsy();
 let denied=false;try{fs.writeFileSync('/work/playwright.config.cjs','changed')}catch(e){denied=true}expect(denied).toBeTruthy();
 let hostReachable=true;try{await page.request.get('http://%s',{timeout:1000})}catch(e){hostReachable=false}expect(hostReachable).toBeFalsy();
 await page.setContent('<h1>Saved</h1>');await expect(page.getByRole('heading')).toHaveText('Saved');
});`, listener.Addr().String())
	for path, data := range map[string]string{"playwright.config.cjs": config, "settings.spec.cjs": spec} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pin := func(env string) Tool {
		t.Helper()
		path := os.Getenv(env)
		if path == "" {
			t.Fatalf("%s required", env)
		}
		tool := Tool{Path: path}
		hash, err := copyTool(ctx, tool, filepath.Join(t.TempDir(), "copy"))
		if err != nil {
			t.Fatal(err)
		}
		tool.SHA256 = hash
		return tool
	}
	platform, err := currentPlatform()
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{SchemaVersion: "1.0.0", Platform: platform, Config: "playwright.config.cjs", SourceRoots: []string{"playwright.config.cjs", "settings.spec.cjs"}, Node: pin("GSB_E2E_NODE"), Modules: pin("GSB_E2E_MODULES"), Browsers: pin("GSB_E2E_BROWSERS"), System: pin("GSB_E2E_SYSTEM"), Isolator: pin("GSB_E2E_ISOLATOR"), TimeoutSeconds: 60}
	request := Request{Files: fileview.Disk{Root: root}, Profile: profile, Mode: "collection", Binding: Binding{RuntimeID: "loop-fixture", Generation: 1, Round: 1, SourceCommit: strings.Repeat("a", 40), SubjectDigest: strings.Repeat("b", 64)}}
	observation, err := run(ctx, request)
	if err != nil {
		t.Fatalf("collection: %v\n%s", err, observation.Bytes())
	}
	collection, ok := observation.Receipt()
	if !ok || !collection.Complete || len(collection.Tests) != 1 || collection.Outcome != "pass" {
		t.Fatalf("collection: %+v", collection)
	}
	if directory := os.Getenv("GSB_E2E_RECEIPT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "collection.json"), observation.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if collection.Platform != platform || collection.Tools["system"] != profile.System.SHA256 || collection.Tools["isolator"] != profile.Isolator.SHA256 {
		t.Fatal("collection omitted actual platform/system/isolator identity")
	}
	fakeDir := t.TempDir()
	fake := filepath.Join(fakeDir, "bwrap")
	marker := filepath.Join(fakeDir, "host-command-executed")
	script := []byte("#!/bin/sh\nprintf unsafe > '" + marker + "'\nexit 1\n")
	if err := os.WriteFile(fake, script, 0700); err != nil {
		t.Fatal(err)
	}
	request.Profile.Isolator = Tool{Path: fake, SHA256: digest(script)}
	if _, err := run(ctx, request); err == nil || !strings.Contains(err.Error(), "platform-owned") {
		t.Fatalf("arbitrary host isolation command was not rejected at the platform boundary: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("project-selected host command executed")
	}
	request.Profile.Isolator = profile.Isolator
	request.Profile.Platform.KernelRelease += "-unapproved"
	if _, err := run(ctx, request); err == nil {
		t.Fatal("unapproved host platform accepted")
	}
	request.Profile.Platform = platform
	request.Profile.System.SHA256 = strings.Repeat("0", 64)
	if _, err := run(ctx, request); err == nil {
		t.Fatal("wrong pinned system closure accepted")
	}
	request.Profile.System = profile.System
	if collection.Tests[0].CaseRefs[0].CaseID != "CASE-001" {
		t.Fatalf("annotations: %+v", collection.Tests)
	}
	request.Mode = "execution"
	request.Collection = &collection
	request.SelectedTestIDs = []string{collection.Tests[0].ID}
	observation, err = run(ctx, request)
	if err != nil {
		t.Fatalf("execution: %v\n%s", err, observation.Bytes())
	}
	execution, ok := observation.Receipt()
	if !ok || !execution.Complete || execution.Outcome != "pass" || len(execution.Attempts) != 1 {
		t.Fatalf("execution: %s", observation.Bytes())
	}
	if directory := os.Getenv("GSB_E2E_RECEIPT_DIR"); directory != "" {
		if err := os.WriteFile(filepath.Join(directory, "execution.json"), observation.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, "playwright.config.cjs")); err != nil || string(got) != config {
		t.Fatal("isolated child changed host source")
	}
	// Exercise the sole exported execution entry using actual Git authority.
	// Dirty local executable bytes must not replace the committed candidate.
	view, profileRef := testFormalView(t, root, profile)
	formal := FormalRequest{Files: view, ProfileRef: profileRef, Binding: request.Binding, Mode: "collection"}
	formal.Binding.SourceCommit = view.Commit
	formal.Binding.AssignmentID = ""
	if err := os.WriteFile(filepath.Join(root, "settings.spec.cjs"), []byte("throw new Error('dirty test must not execute');"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ProfilePath), []byte(`{"node":{"path":"/bin/sh"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"collection", "execution"} {
		formal.Mode = mode
		capture, err := PrepareCapture(ctx, root, formal)
		if err != nil {
			t.Fatal(err)
		}
		observed, runErr := capture.Run(ctx)
		if err := capture.Close(); err != nil {
			t.Fatal(err)
		}
		if runErr != nil {
			t.Fatalf("formal %s: %v\n%s", mode, runErr, observed.Bytes())
		}
		r, ok := observed.Receipt()
		if !ok || !r.Complete || !r.Invoked || r.Outcome != "pass" || r.Stage != "completed" || r.ProfileRef == nil || *r.ProfileRef != profileRef {
			t.Fatalf("formal receipt missing execution/source identity: %s", observed.Bytes())
		}
		interrupted, err := InspectInterruptedCapture(root, capture.IntentBytes())
		if err != nil || interrupted.Outcome != "unknown" || interrupted.RawEvents != r.RawEvents || interrupted.Output != r.Output {
			t.Fatalf("live output differed from persistent capture or saved bytes gained PASS authority: %+v %v", interrupted, err)
		}
		if directory := os.Getenv("GSB_E2E_RECEIPT_DIR"); directory != "" {
			if err := os.WriteFile(filepath.Join(directory, "formal_"+mode+".json"), observed.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "collection" {
			formal.Collection = &r
			formal.SelectedTestIDs = []string{r.Tests[0].ID}
			formal.Binding.AssignmentID = "assignment-e2e"
		}
	}
	testGit(t, root, "commit", "--allow-empty", "-qm", "Advance branch after observation")
	if _, err := RunFormal(ctx, formal); err == nil {
		t.Fatal("source branch moved but old authority view still ran")
	}
	if err := os.WriteFile(filepath.Join(root, "settings.spec.cjs"), []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	request.Binding.Round++
	if _, err = run(ctx, request); err == nil {
		t.Fatal("old collection accepted in a new round")
	}
	request.Binding.Round--
	// Source drift is rejected before starting an execution, rather than
	// consuming a prior collection against a different spec.
	if err := os.WriteFile(filepath.Join(root, "settings.spec.cjs"), []byte("// CASE-001: comment only, no tests\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(ctx, request); err == nil {
		t.Fatal("changed spec reused old collection")
	}
	request.Mode, request.Collection, request.SelectedTestIDs = "collection", nil, nil
	observation, err = run(ctx, request)
	if err == nil {
		t.Fatal("comment-only spec was admitted as collection")
	}
	if r, ok := observation.Receipt(); !ok || r.Complete || r.Outcome == "pass" {
		t.Fatalf("zero collection lost its failure receipt: %s", observation.Bytes())
	}
	// --list executes imports. A config attempting to write a product file
	// must fail inside the same readonly namespace as execution.
	if err := os.WriteFile(filepath.Join(root, "playwright.config.cjs"), []byte("require('fs').writeFileSync('/work/settings.spec.cjs','forged');\n"+config), 0644); err != nil {
		t.Fatal(err)
	}
	if observation, err = run(ctx, request); err == nil {
		t.Fatal("collection import wrote into its readonly product snapshot")
	}
	if data, err := os.ReadFile(filepath.Join(root, "settings.spec.cjs")); err != nil || string(data) != "// CASE-001: comment only, no tests\n" {
		t.Fatal("collection changed the host input")
	}
	exerciseFormalFailures(t, ctx, root, profile, config, spec, formal)
	exerciseCapturedSIGKILL(t, ctx, profile, config)
}
