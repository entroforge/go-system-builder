// Package diagnosticstorage recognizes bounded display storage, never receipts
// or source authority. Unknown files beside diagnostics remain product files.
package diagnosticstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

var finalPath = regexp.MustCompile(`^\.claude/diagnostics/(planning|native-s9)/[0-9a-f]{64}\.json$`)
var temporaryPath = regexp.MustCompile(`^\.claude/diagnostics/(planning|native-s9)/\.diagnostic-[0-9a-f]{32}\.tmp$`)

func Recognizes(ctx context.Context, root, path string) bool {
	if path == ".claude/diagnostics/native-s9/.publish.lock" {
		if ctx.Err() != nil || workpacket.CheckPath(root, path) != nil {
			return false
		}
		info, err := os.Lstat(root + "/" + path)
		return err == nil && info.Mode().IsRegular() && info.Size() == 0
	}
	final, temporary := finalPath.MatchString(path), temporaryPath.MatchString(path)
	if (!final && !temporary) || ctx.Err() != nil || workpacket.CheckPath(root, path) != nil {
		return false
	}
	data, err := workpacket.ReadFile(root, path, 64<<10)
	if ctx.Err() != nil {
		return false
	}
	if temporary {
		// The producer may still be writing, or have renamed it after status.
		return err == nil || os.IsNotExist(err)
	}
	if err != nil {
		return false
	}
	// A closed top-level shape also rejects duplicate keys and trailing JSON.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || fields[name] != nil {
			return false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return false
	}
	text := func(key string) string { var s string; _ = json.Unmarshal(fields[key], &s); return s }
	if text("message") == "" {
		return false
	}
	if strings.Contains(path, "/planning/") {
		_, err := time.Parse(time.RFC3339Nano, text("at"))
		return len(fields) == 2 && err == nil
	}
	var files map[string]string
	if len(text("scope")) != 64 || text("runtime") == "" || json.Unmarshal(fields["files"], &files) != nil || files == nil {
		return false
	}
	if len(fields) == 4 {
		return fields["attempt"] == nil
	}
	if len(fields) != 5 && len(fields) != 6 || fields["attempt"] == nil {
		return false
	}
	if !recognizesAttempt(fields["attempt"]) {
		return false
	}
	if len(fields) == 5 {
		return fields["consumer_failure"] == nil
	}
	return recognizesConsumerFailure(fields["consumer_failure"])
}

// Match only the producer's fixed, string-valued attempt schema. Duplicate
// keys and extensions are product files, not an ignored diagnostic shape.
func recognizesAttempt(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return false
		}
		if _, duplicate := fields[name]; duplicate {
			return false
		}
		var value string
		if decoder.Decode(&value) != nil {
			return false
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return false
	}
	if len(fields) != 8 {
		return false
	}
	for _, name := range []string{"kind", "responsibility_ref", "owner", "stage", "input_ref", "typed_input_ref", "failure_kind", "observed_at"} {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	if fields["kind"] != "Builder" && fields["kind"] != "Targeted" {
		return false
	}
	if fields["responsibility_ref"] == "" || fields["owner"] == "" || fields["stage"] == "" {
		return false
	}
	switch fields["failure_kind"] {
	case "deadline", "canceled", "successor", "conflict", "author", "unknown":
	default:
		return false
	}
	_, err = time.Parse(time.RFC3339Nano, fields["observed_at"])
	return err == nil
}

// The retained reason has exactly the same bounded observation shape, without
// recursively retaining earlier attempts or accepting product metadata.
func recognizesConsumerFailure(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || fields[name] != nil {
			return false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return false
	}
	var message string
	var files map[string]string
	return len(fields) == 3 && json.Unmarshal(fields["message"], &message) == nil && message != "" && json.Unmarshal(fields["files"], &files) == nil && files != nil && recognizesAttempt(fields["attempt"])
}
