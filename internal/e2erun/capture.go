package e2erun

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const captureBase = ".claude/operations/e2e-runs"

// CaptureIntent describes an attempt BEFORE execution, not a browser result.
// The Runtime producer must commit these exact bytes in its existing journal
// and artifact transaction before calling Capture.Run. Capture files are only
// recovery material; they are never a second authority for completion/PASS.
type CaptureIntent struct {
	Version          string     `json:"schema_version"`
	RunID            string     `json:"run_id"`
	Mode             string     `json:"mode"`
	Binding          Binding    `json:"binding"`
	ProfileRef       FileDigest `json:"profile_ref"`
	CollectionSHA256 string     `json:"collection_sha256,omitempty"`
	SelectedTestIDs  []string   `json:"selected_test_ids,omitempty"`
	PreparedAt       string     `json:"prepared_at"`
}

// Capture is an operation-private, exclusive persistent raw-output sink. The
// token cannot be populated from JSON. No method imports saved bytes as an
// Observation; only its single actual Run can return one.
type Capture struct {
	mu         sync.Mutex
	used       bool
	closed     bool
	root       *os.Root
	rel        string
	intent     CaptureIntent
	intentData []byte
	request    FormalRequest
	events     *durableCaptureWriter
	output     *durableCaptureWriter
}

// PrepareCapture creates private recovery inputs without running project code.
// No approval or Runtime registration is implied. Failed preparation leaves its
// exclusive directory intact: only a future lease/reachability-aware collector
// may remove it, never a generic failed-command cleanup path.
func PrepareCapture(ctx context.Context, root string, request FormalRequest) (*Capture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Files == nil {
		return nil, fmt.Errorf("capture requires a formal source view")
	}
	// Freeze exported root/ref/commit fields as well as the private source rules.
	request.Files = request.Files.WithContext(ctx)
	actual, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	declared, err := filepath.EvalSymlinks(request.Files.Root)
	if err != nil || actual != declared {
		return nil, fmt.Errorf("capture root differs from source authority")
	}
	if _, err := ReadFormalProfile(request.Files.WithContext(ctx), request.ProfileRef); err != nil {
		return nil, err
	}
	if request.Binding.SourceCommit != request.Files.Commit || request.Binding.RuntimeID == "" || request.Binding.Round < 1 || request.Binding.Generation < 0 || len(request.Binding.SubjectDigest) != 64 {
		return nil, fmt.Errorf("capture binding is incomplete or differs from the formal source")
	}
	if request.Mode != "collection" && request.Mode != "execution" {
		return nil, fmt.Errorf("unknown capture mode %s", request.Mode)
	}
	if request.Mode == "collection" && (request.Collection != nil || len(request.SelectedTestIDs) != 0) {
		return nil, fmt.Errorf("collection capture cannot include an execution selection")
	}
	// Freeze caller-owned slices/receipts before the producer commits the intent.
	request.SelectedTestIDs = append([]string(nil), request.SelectedTestIDs...)
	collectionSHA := ""
	if request.Collection != nil {
		data, err := json.Marshal(request.Collection)
		if err != nil {
			return nil, err
		}
		collection, err := DecodeReceipt(data)
		if err != nil {
			return nil, err
		}
		if collection.Mode != "collection" || !collection.Complete || collection.Outcome != "pass" {
			return nil, fmt.Errorf("execution capture requires a complete successful collection")
		}
		binding := request.Binding
		binding.AssignmentID = collection.Binding.AssignmentID
		if collection.Binding != binding || collection.ProfileRef == nil || *collection.ProfileRef != request.ProfileRef {
			return nil, fmt.Errorf("capture collection authority differs from request")
		}
		byID, selected := map[string]bool{}, map[string]bool{}
		for _, test := range collection.Tests {
			byID[test.ID] = true
		}
		for _, id := range request.SelectedTestIDs {
			if !byID[id] || selected[id] {
				return nil, fmt.Errorf("capture selection has duplicate or uncollected test ID %q", id)
			}
			selected[id] = true
		}
		request.Collection = &collection
		collectionSHA = digest(data)
	}
	if request.Mode == "execution" && (request.Collection == nil || len(request.SelectedTestIDs) == 0) {
		return nil, fmt.Errorf("execution capture requires its indexed collection and exact selected set")
	}
	sort.Strings(request.SelectedTestIDs)
	if err := request.Files.Verify(); err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	intent := CaptureIntent{Version: "1.0.0", RunID: "e2e-run-" + hex.EncodeToString(id), Mode: request.Mode, Binding: request.Binding, ProfileRef: request.ProfileRef, CollectionSHA256: collectionSHA, SelectedTestIDs: append([]string(nil), request.SelectedTestIDs...), PreparedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeCaptureIntent(data); err != nil {
		return nil, err
	}
	anchor, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	c := &Capture{root: anchor, rel: captureBase + "/" + intent.RunID, intent: intent, intentData: data, request: request}
	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = checkCapturePath(anchor, c.rel); err != nil {
		return nil, err
	}
	if err = anchor.MkdirAll(captureBase, 0700); err != nil {
		return nil, err
	}
	if err = anchor.Mkdir(c.rel, 0700); err != nil {
		return nil, err
	}
	if err = c.writeExclusive("intent.json", data); err != nil {
		return nil, err
	}
	for _, target := range []struct {
		name string
		dest **durableCaptureWriter
	}{{"events.jsonl", &c.events}, {"output.log", &c.output}} {
		var file *os.File
		file, err = anchor.OpenFile(c.rel+"/"+target.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		*target.dest = &durableCaptureWriter{file: file}
		if err = file.Sync(); err != nil {
			return nil, err
		}
	}
	if err = c.syncDirectories(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Capture) IntentBytes() []byte { return append([]byte(nil), c.intentData...) }

// Run is single-use, even after an error. The caller must first durably commit
// IntentBytes and retain the operation lease. A retry must query that Runtime
// intent/completion rather than call this method again or create another run.
func (c *Capture) Run(ctx context.Context) (*Observation, error) {
	c.mu.Lock()
	if c.used || c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("capture was already used/closed; query the original Runtime operation")
	}
	c.used = true
	c.mu.Unlock()
	request := c.request
	request.capture = c
	observation, runErr := RunFormal(ctx, request)
	if observation == nil {
		return nil, runErr
	}
	if err := c.writeExclusive("returned-observation.json", observation.Bytes()); err != nil {
		// Do not advertise a durable capture when its final write failed.
		receipt, _ := observation.Receipt()
		receipt.Complete, receipt.Outcome = false, "unknown"
		receipt.Problems = append(receipt.Problems, "capture finalization failed: "+err.Error())
		data, _ := json.Marshal(receipt)
		return &Observation{data: data}, errors.Join(runErr, err)
	}
	return observation, runErr
}

func (c *Capture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for _, writer := range []*durableCaptureWriter{c.events, c.output} {
		if writer != nil {
			errs = append(errs, writer.Close())
		}
	}
	if c.root != nil {
		errs = append(errs, c.root.Close())
	}
	return errors.Join(errs...)
}

func (c *Capture) writeExclusive(name string, data []byte) error {
	file, err := c.root.OpenFile(c.rel+"/"+name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	return c.syncDirectories()
}

func (c *Capture) syncDirectories() error {
	for current := c.rel; ; current = path.Dir(current) {
		f, err := c.root.Open(current)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
		if current == "." {
			break
		}
	}
	return nil
}

type durableCaptureWriter struct {
	mu   sync.Mutex
	file *os.File
}

func (w *durableCaptureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.file.Write(data)
	if err == nil {
		err = w.file.Sync()
	}
	return n, err
}
func (w *durableCaptureWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// InterruptedCapture is recovery material, deliberately NOT an Observation.
// Even a fully saved PASS cannot acquire execution evidence through this
// reader. Only a committed Runtime receipt may replay a successful operation.
type InterruptedCapture struct {
	Intent    CaptureIntent `json:"intent"`
	Outcome   string        `json:"outcome"`
	RawEvents string        `json:"raw_events"`
	Output    string        `json:"output"`
}

// InspectInterruptedCapture must be called under the Runtime producer's
// per-operation OS lease, using the exact intent bytes from its immutable
// registered artifact. It neither resumes execution nor grants PASS.
func InspectInterruptedCapture(root string, expectedIntent []byte) (InterruptedCapture, error) {
	intent, err := DecodeCaptureIntent(expectedIntent)
	if err != nil {
		return InterruptedCapture{}, err
	}
	return inspectInterruptedCapture(root, expectedIntent, intent)
}

// DecodeCaptureIntent validates recovery input; it does not authenticate the
// caller or prove that the intent was committed to Runtime before invocation.
func DecodeCaptureIntent(data []byte) (CaptureIntent, error) {
	var intent CaptureIntent
	if err := schema.NewEmbeddedValidator().ValidateBytes("e2e-capture-intent.schema.json", data); err != nil {
		return intent, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return intent, fmt.Errorf("capture intent must be one JSON object")
	}
	if _, err := time.Parse(time.RFC3339Nano, intent.PreparedAt); err != nil {
		return intent, fmt.Errorf("invalid capture preparation time: %w", err)
	}
	return intent, nil
}

func inspectInterruptedCapture(root string, expectedIntent []byte, intent CaptureIntent) (InterruptedCapture, error) {
	anchor, err := os.OpenRoot(root)
	if err != nil {
		return InterruptedCapture{}, err
	}
	defer anchor.Close()
	rel := captureBase + "/" + intent.RunID
	if err := checkCapturePath(anchor, rel); err != nil {
		return InterruptedCapture{}, err
	}
	read := func(name string) ([]byte, error) {
		if err := checkCapturePath(anchor, rel+"/"+name); err != nil {
			return nil, err
		}
		f, err := anchor.Open(rel + "/" + name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("capture input is not a regular file: %s", name)
		}
		data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		if len(data) > 8<<20 {
			return nil, fmt.Errorf("capture file exceeds its bounded output size")
		}
		return data, err
	}
	actual, err := read("intent.json")
	if err != nil || !bytes.Equal(actual, expectedIntent) {
		return InterruptedCapture{}, fmt.Errorf("capture intent differs from its registered immutable bytes: %v", err)
	}
	events, err := read("events.jsonl")
	if err != nil {
		return InterruptedCapture{}, err
	}
	output, err := read("output.log")
	if err != nil {
		return InterruptedCapture{}, err
	}
	return InterruptedCapture{Intent: intent, Outcome: "unknown", RawEvents: string(events), Output: string(output)}, nil
}

func checkCapturePath(root *os.Root, rel string) error {
	if rel != path.Clean(rel) || !strings.HasPrefix(rel, captureBase+"/") || strings.Contains(rel, "\\") {
		return fmt.Errorf("unsafe capture path %s", rel)
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("capture path contains a symlink or non-directory component")
		}
	}
	return nil
}
