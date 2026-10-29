package hook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const ProgressRepeatThreshold = 5

// ProgressFacts is an adapter projection of original responsibility facts.
// It is diagnostic input, never an authorization or completion certificate.
type ProgressFacts struct {
	Binding       string
	Owner         string
	Session       string
	Semantic      string
	Gap           string
	RecoveryOwner string
	Waiting       bool
	Running       bool
	Complete      bool
	Known         bool
	Sources       map[string]string // canonical absolute source -> actual digest
}

type ProgressEvent struct {
	Action string
	Read   *ProgressRead
	Known  bool
}

type ProgressRead struct {
	Path, Digest, Range string
}

type ProgressNotice struct {
	Version          string   `json:"version"`
	Kind             string   `json:"kind"`
	CurrentGap       string   `json:"current_gap"`
	RecoveryOwner    string   `json:"recovery_owner"`
	LastProgressAt   string   `json:"last_progress_at"`
	LastProgressKind string   `json:"last_progress_kind"`
	AttemptedActions []string `json:"attempted_actions"`
	Repeats          int      `json:"repeats"`
	Reason           string   `json:"reason"`
	Authorizes       bool     `json:"authorizes"`
}

type progressRecord struct {
	Notice           *ProgressNotice `json:"notice,omitempty"`
	Coverage         string          `json:"coverage"`
	Version          int             `json:"version"`
	Binding          string          `json:"binding"`
	Semantic         string          `json:"semantic"`
	Gap              string          `json:"gap"`
	Reads            map[string]bool `json:"reads"`
	Repeats          int             `json:"repeats"`
	Notified         bool            `json:"notified"`
	Saturated        bool            `json:"saturated"`
	LastProgressAt   string          `json:"last_progress_at"`
	LastProgressKind string          `json:"last_progress_kind"`
	Actions          []string        `json:"actions"`
}

func progressHash(data string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(data))) }

func progressAction(action string) bool {
	switch action {
	case "Read", "status", "Glob", "Grep", "Write", "Edit", "MultiEdit", "submission":
		return true
	}
	return false
}

// ObserveWorkProgress writes only one bounded cache slot per runtime/owner/session.
// Unknown observations reset the diagnostic count; loss of telemetry is never
// evidence of stagnation. Wall time is descriptive, not a liveness deadline.
func ObserveWorkProgress(root string, facts ProgressFacts, event ProgressEvent) (*ProgressNotice, bool, bool, error) {
	return observeWorkProgress(root, facts, event, 25*time.Millisecond)
}

func observeWorkProgress(root string, facts ProgressFacts, event ProgressEvent, lockBudget time.Duration) (*ProgressNotice, bool, bool, error) {
	if facts.Binding == "" || facts.Owner == "" || facts.Session == "" || !facts.Known {
		return nil, false, false, nil
	}
	key := progressHash(facts.Owner + "\x00" + facts.Session)
	ref := ".claude/hook-notices/work-progress/" + key + ".json"
	parent, name, err := workpacket.OpenParent(root, ref, true)
	if err != nil {
		return nil, false, false, err
	}
	defer parent.Close()
	if err := workpacket.CheckPath(root, ref); err != nil {
		return nil, false, false, err
	}
	lockRef := ref + ".lck"
	if err := workpacket.CheckPath(root, lockRef); err != nil {
		return nil, false, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockBudget)
	defer cancel()
	release, err := filelock.Acquire(ctx, filepath.Join(root, filepath.FromSlash(lockRef)))
	if err != nil {
		return nil, false, false, fmt.Errorf("progress diagnostic cache unavailable: %w", err)
	}
	defer release()
	r := progressRecord{}
	data, err := workpacket.ReadFile(root, ref, 64<<10)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&r)
		var extra any
		if decodeErr != nil || decoder.Decode(&extra) != io.EOF || r.Version != 1 || r.Reads == nil || len(r.Reads) > 64 || len(r.Actions) > 8 || r.Repeats < 0 || r.Repeats > ProgressRepeatThreshold || len(r.LastProgressAt) > 40 || (r.LastProgressKind != "responsibility_facts" && r.LastProgressKind != "necessary_source_range") {
			return nil, false, false, fmt.Errorf("progress diagnostic cache damaged; Main may remove only this cache slot")
		}
		for _, action := range r.Actions {
			if !progressAction(action) {
				return nil, false, false, fmt.Errorf("progress cache action vocabulary invalid")
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, false, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	binding, semantic, gap := progressHash(facts.Binding), progressHash(facts.Semantic), progressHash(facts.Gap)
	changed := r.Version == 0 || r.Binding != binding || r.Semantic != semantic || r.Gap != gap
	factsChanged := changed
	if changed {
		r = progressRecord{Version: 1, Binding: binding, Semantic: semantic, Gap: gap, Reads: map[string]bool{}, LastProgressAt: now, LastProgressKind: "responsibility_facts", Actions: []string{}}
	}
	known := event.Known && progressAction(event.Action)
	if event.Read != nil {
		read := event.Read
		if facts.Sources[read.Path] != read.Digest || read.Digest == "" || read.Range == "" {
			known = false
		} else {
			key := progressHash(read.Path + "\x00" + read.Digest + "\x00" + read.Range)
			if !r.Reads[key] {
				if len(r.Reads) == 64 {
					r.Saturated = true
					known = false
				} else {
					r.Reads[key] = true
					r.Repeats, r.Notified = 0, false
					r.LastProgressAt, r.LastProgressKind = now, "necessary_source_range"
					changed = true
				}
			}
		}
	}
	if known {
		found := false
		for _, action := range r.Actions {
			found = found || action == event.Action
		}
		if !found {
			r.Actions = append(r.Actions, event.Action)
		}
	}
	// A read without a proven actual source/range is incomplete coverage.
	if event.Action == "Read" && event.Read == nil {
		known = false
	}
	if !known || facts.Waiting || facts.Running || facts.Complete || r.Saturated {
		r.Repeats = 0
	} else if !changed && (event.Action == "Read" || event.Action == "status") && r.Repeats < ProgressRepeatThreshold {
		r.Repeats++
	}
	r.Coverage = "complete"
	if !known {
		r.Coverage = "incomplete"
	}
	if r.Saturated {
		r.Coverage = "saturated"
	}
	var notice *ProgressNotice
	if known && (event.Action == "Read" || event.Action == "status") && r.Repeats == ProgressRepeatThreshold && !r.Notified && facts.Gap != "" {
		owner := facts.RecoveryOwner
		if owner == "" {
			owner = "Main"
		}
		notice = &ProgressNotice{Version: "1.0.0", Kind: "stall", CurrentGap: progressBound(facts.Gap, 128), RecoveryOwner: progressBound(owner, 64), LastProgressAt: r.LastProgressAt, LastProgressKind: r.LastProgressKind, AttemptedActions: append([]string{}, r.Actions...), Repeats: r.Repeats, Reason: "same responsibility gap and professional facts across repeated supported observations", Authorizes: false}
		r.Notified = true
		r.Notice = notice
	}
	data, err = json.Marshal(r)
	if err != nil || len(data) > 64<<10 {
		return nil, false, false, fmt.Errorf("progress cache exceeds bound")
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, false, false, err
	}
	temporary := fmt.Sprintf(".progress-%x", id)
	f, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, false, false, err
	}
	defer parent.Remove(temporary)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = parent.Rename(temporary, name)
	}
	if err != nil {
		return nil, false, false, err
	}
	return notice, true, factsChanged, nil
}

func progressBound(s string, bytes int) string {
	if len(s) <= bytes {
		return s
	}
	for bytes > 0 && (s[bytes]&0xc0) == 0x80 {
		bytes--
	}
	return strings.TrimSpace(s[:bytes])
}
