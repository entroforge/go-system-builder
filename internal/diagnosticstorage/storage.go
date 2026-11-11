// Package diagnosticstorage recognizes bounded display storage, never receipts
// or source authority. Unknown files beside diagnostics remain product files.
package diagnosticstorage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const planningPrefix = ".claude/diagnostics/planning/"

// The producers write display storage in three observable layouts: planning
// entries named by the reversible base64url scope, the historical 64-hex
// names, and the segmented native-s9 responsibility path. These are shapes of
// the producer's own reversible encoding, never digests of content.
var finalPath = regexp.MustCompile(`^\.claude/diagnostics/(?:planning/[0-9A-Za-z_-]+\.json|native-s9/[0-9a-f]{64}\.json|native-s9/responsibility/[0-9A-Za-z_-]+(?:/[0-9A-Za-z_-]+)*/observation\.json)$`)
var temporaryPath = regexp.MustCompile(`^\.claude/diagnostics/(?:planning/\.diagnostic-[0-9a-f]{32}\.tmp|native-s9/\.diagnostic-[0-9a-f]{32}\.tmp|native-s9/responsibility/[0-9A-Za-z_-]+(?:/[0-9A-Za-z_-]+)*/\.diagnostic-[0-9a-f]{32}\.tmp)$`)

// The historical 64-hex identity shape, read as a shape only.
var hexIdentity = regexp.MustCompile(`^[0-9a-f]{64}$`)

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
	if final && strings.HasPrefix(path, planningPrefix) {
		// A planning entry is named by its reversible scope. Unknown neighbours
		// keep their own names and stay product files.
		name := strings.TrimSuffix(strings.TrimPrefix(path, planningPrefix), ".json")
		if !recognizesPlanningName(name) {
			return false
		}
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
	if !recognizesScope(text("scope")) || text("runtime") == "" || json.Unmarshal(fields["files"], &files) != nil || files == nil {
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

// A planning entry is named either by the historical 64-hex identity or by the
// producer's reversible base64url scope. The reversible name must decode to a
// coordinate JSON array, so an arbitrary neighbour name stays a product file.
func recognizesPlanningName(name string) bool {
	if hexIdentity.MatchString(name) {
		return true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(name)
	if err != nil {
		return false
	}
	var coordinates []any
	return json.Unmarshal(decoded, &coordinates) == nil && len(coordinates) > 0
}

// The scope is a display association, not a proof of bytes. It is either the
// historical 64-hex shape or the producer's coordinate JSON array. No digest
// is recomputed and no recorded value is compared.
func recognizesScope(scope string) bool {
	if scope == "" {
		return false
	}
	if hexIdentity.MatchString(scope) {
		return true
	}
	var coordinates []any
	return json.Unmarshal([]byte(scope), &coordinates) == nil && len(coordinates) > 0
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
	// Historical attempts carry eight fields; the current producer adds the
	// explicit consumer boundary marker as a ninth string field.
	if len(fields) != 8 && len(fields) != 9 {
		return false
	}
	for _, name := range []string{"kind", "responsibility_ref", "owner", "stage", "input_ref", "typed_input_ref", "failure_kind", "observed_at"} {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	if len(fields) == 9 {
		boundary, ok := fields["consumer_boundary"]
		if !ok || (boundary != "1" && boundary != "") {
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
