package e2erun

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

//go:embed reporter.cjs
var reporter []byte

type Request struct {
	Files   fileview.Reader
	Profile Profile
	Binding Binding
	Mode    string
	// Collection must be loaded from indexed evidence by the producer. It is
	// a selection contract, never itself proof that execution happened.
	Collection      *Receipt
	SelectedTestIDs []string
	capture         *Capture
}

// run never takes an arbitrary shell command or imports a caller receipt.
// All executable tools must be hash-pinned by the authority choosing Profile.
// Missing isolation is an error, never permission for an unsandboxed retry.
func run(ctx context.Context, request Request) (observation *Observation, runError error) {
	profileData, err := json.Marshal(request.Profile)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeProfile(profileData); err != nil {
		return nil, fmt.Errorf("runner profile: %w", err)
	}
	if request.Files == nil || len(request.Profile.SourceRoots) == 0 {
		return nil, fmt.Errorf("declared input reader and source roots are required")
	}
	if request.Mode != "collection" && request.Mode != "execution" {
		return nil, fmt.Errorf("unknown E2E mode %q", request.Mode)
	}
	if request.Mode == "collection" && (request.Collection != nil || len(request.SelectedTestIDs) != 0) {
		return nil, fmt.Errorf("collection cannot include an execution selection")
	}
	if request.Binding.RuntimeID == "" || request.Binding.SourceCommit == "" || request.Binding.Round < 1 || request.Binding.Generation < 0 || len(request.Binding.SubjectDigest) != 64 {
		return nil, fmt.Errorf("E2E invocation requires bound Runtime/generation/round/source/subject")
	}
	// Once a structurally bound attempt is accepted, every preparation or
	// infrastructure failure remains an observation. Invoked is false until
	// the isolation adapter is called; a failure is never a browser PASS.
	attempted := time.Now().UTC()
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	r := Receipt{Version: "1.0.0", RunID: "e2e-run-" + hex.EncodeToString(id), Mode: request.Mode, Binding: request.Binding, ProfileSHA256: digest(profileData), Inputs: []FileDigest{}, Tools: map[string]string{}, Tests: []Test{}, Attempts: []Attempt{}, Problems: []string{}, Outcome: "unknown", ExitCode: -1, Stage: "preparation", AttemptedAt: attempted.Format(time.RFC3339Nano)}
	if request.capture != nil {
		r.RunID = request.capture.intent.RunID
	}
	defer func() {
		if observation != nil || runError == nil {
			return
		}
		r.PreparationMS = time.Since(attempted).Milliseconds()
		r.Problems = append(r.Problems, runError.Error())
		data, encodeErr := json.Marshal(r)
		if encodeErr != nil {
			runError = fmt.Errorf("%v; encode failed attempt: %w", runError, encodeErr)
			return
		}
		observation = &Observation{data: data}
	}()
	if request.Profile.TimeoutSeconds < 1 || request.Profile.TimeoutSeconds > 1800 {
		return nil, fmt.Errorf("timeout_seconds must be 1..1800")
	}
	config, err := relative(request.Profile.Config)
	if err != nil {
		return nil, err
	}
	if err := validateIsolator(request.Profile.Isolator); err != nil {
		return nil, err
	}
	platform, err := currentPlatform()
	if err != nil {
		return nil, err
	}
	r.Platform = platform
	if request.Profile.Platform != platform {
		return nil, fmt.Errorf("E2E platform differs from the approved profile: expected %+v, actual %+v", request.Profile.Platform, platform)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.Profile.TimeoutSeconds)*time.Second)
	defer cancel()
	staging, err := os.MkdirTemp("", "loop-e2e-run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	work := filepath.Join(staging, "work")
	if err = os.Mkdir(work, 0700); err != nil {
		return nil, err
	}
	// A single canonical module path avoids loading two Playwright singletons
	// through duplicate bind-mount aliases. Modules includes the full pinned
	// dependency closure needed by config, tests and the local application.
	if err = os.Symlink("/opt/modules", filepath.Join(work, "node_modules")); err != nil {
		return nil, err
	}
	inputs, inputHash, err := snapshot(ctx, request.Files, request.Profile.SourceRoots, work)
	if err != nil {
		return nil, err
	}
	configFound := false
	for _, input := range inputs {
		configFound = configFound || input.Path == config
	}
	if !configFound {
		return nil, fmt.Errorf("runner config is outside declared input snapshot")
	}
	r.Inputs, r.InputSHA256 = inputs, inputHash
	selected := map[string]bool{}
	list := ""
	if request.Mode == "execution" {
		collection := request.Collection
		if collection == nil || collection.Mode != "collection" || !collection.Complete || collection.Outcome != "pass" {
			return nil, fmt.Errorf("execution requires an actual successful runner collection Observation")
		}
		if collection.Binding.RuntimeID != r.Binding.RuntimeID || collection.Binding.Generation != r.Binding.Generation || collection.Binding.Round != r.Binding.Round || collection.Binding.SourceCommit != r.Binding.SourceCommit || collection.Binding.SubjectDigest != r.Binding.SubjectDigest || collection.ProfileSHA256 != r.ProfileSHA256 || collection.InputSHA256 != r.InputSHA256 {
			return nil, fmt.Errorf("collection no longer matches execution authority and inputs")
		}
		collectionBytes, _ := json.Marshal(collection)
		r.CollectionSHA256 = digest(collectionBytes)
		byID := map[string]Test{}
		for _, test := range collection.Tests {
			byID[test.ID] = test
		}
		for _, testID := range request.SelectedTestIDs {
			if selected[testID] {
				return nil, fmt.Errorf("duplicate selected test ID %s", testID)
			}
			selected[testID] = true
			test, ok := byID[testID]
			if !ok {
				return nil, fmt.Errorf("selected test ID is absent from collection: %s", testID)
			}
			parts := append([]string{test.Project, test.SelectionPath}, test.TitlePath...)
			for _, part := range parts {
				if strings.ContainsAny(part, "\n\r>›") {
					return nil, fmt.Errorf("test selector cannot be represented exactly: %s", testID)
				}
			}
			list += "[" + test.Project + "] › " + test.SelectionPath + " › " + strings.Join(test.TitlePath, " › ") + "\n"
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("execution requires an explicit nonempty selected test set")
		}
		r.SelectedTestIDs = append([]string(nil), request.SelectedTestIDs...)
		sort.Strings(r.SelectedTestIDs)
	}
	for _, tool := range []struct {
		name  string
		value Tool
	}{{"node", request.Profile.Node}, {"modules", request.Profile.Modules}, {"browsers", request.Profile.Browsers}, {"system", request.Profile.System}, {"isolator", request.Profile.Isolator}} {
		if len(tool.value.SHA256) != 64 {
			return nil, fmt.Errorf("%s tool must have a pinned SHA256", tool.name)
		}
		hash, err := copyTool(ctx, tool.value, filepath.Join(staging, tool.name))
		if err != nil {
			return nil, err
		}
		r.Tools[tool.name] = hash
	}
	if _, err := os.Stat(filepath.Join(staging, "modules/playwright/cli.js")); err != nil {
		return nil, fmt.Errorf("pinned modules must contain Playwright CLI: %w", err)
	}
	adapter := filepath.Join(staging, "adapter")
	if err = os.Mkdir(adapter, 0700); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(adapter, "reporter.cjs"), reporter, 0400); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(adapter, "selection.txt"), []byte(list), 0400); err != nil {
		return nil, err
	}
	r.Tools["reporter"] = digest(reporter)
	r.Command = []string{"/opt/node", "/opt/modules/playwright/cli.js", "test", "--config", "/work/" + config, "--reporter", "/adapter/reporter.cjs", "--output", "/tmp/results", "--workers=1", "--retries=0", "--repeat-each=1", "--forbid-only"}
	if request.Mode == "collection" {
		r.Command = append(r.Command, "--list")
	} else {
		r.Command = append(r.Command, "--test-list", "/adapter/selection.txt")
	}
	output, events := &limitedBuffer{cancel: cancel}, &limitedBuffer{cancel: cancel}
	if request.capture != nil {
		output.sink, events.sink = request.capture.output, request.capture.events
	}
	started := time.Now().UTC()
	r.Stage, r.Invoked = "invocation", true
	r.PreparationMS = started.Sub(attempted).Milliseconds()
	r.StartedAt = started.Format(time.RFC3339Nano)
	r.ExitCode, err = runIsolated(ctx, staging, r.Command, output, events)
	r.ElapsedMS = time.Since(started).Milliseconds()
	r.Output = output.String()
	r.RawEvents = events.String()
	if err != nil {
		r.Problems = append(r.Problems, err.Error())
	}
	if output.overflow || events.overflow {
		r.Problems = append(r.Problems, "runner output exceeded 8 MiB limit")
	}
	parseEvents(&r, selected)
	if r.Complete && request.Mode == "execution" {
		byID := map[string]Test{}
		for _, test := range request.Collection.Tests {
			byID[test.ID] = test
		}
		for _, test := range r.Tests {
			if !reflect.DeepEqual(test, byID[test.ID]) {
				r.Problems = append(r.Problems, "execution test identity/annotations differ from collection: "+test.ID)
				r.Complete = false
				r.Outcome = "unknown"
			}
		}
	}
	if r.Complete {
		r.Stage = "completed"
	}
	data, encodeErr := json.Marshal(r)
	if encodeErr != nil {
		return nil, encodeErr
	}
	observation = &Observation{data: data}
	if !r.Complete {
		return observation, fmt.Errorf("E2E invocation incomplete: %s", strings.Join(r.Problems, "; "))
	}
	return observation, nil
}

type limitedBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
	sink     io.Writer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(p) > 8<<20 {
		b.overflow = true
		b.cancel()
		return 0, fmt.Errorf("output limit")
	}
	// These bytes have already been observed from the child. Keep them even
	// when durable capture fails, so a returned UNKNOWN cannot hide the first
	// failure frame that caused a write/fsync error.
	if _, err := b.data.Write(p); err != nil {
		return 0, err
	}
	if b.sink != nil {
		if n, err := b.sink.Write(p); err != nil || n != len(p) {
			b.cancel()
			if err == nil {
				err = io.ErrShortWrite
			}
			return n, err
		}
	}
	return len(p), nil
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }
