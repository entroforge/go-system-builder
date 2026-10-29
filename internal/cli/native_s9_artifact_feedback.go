package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func nativeS9ArtifactAuthorError(kind, path string, err error) error {
	return &nativeAuthorError{fmt.Errorf("actual native %s at %s: %w; correct this private JSON file using the brief; keep professional fields at the top level", kind, path, err)}
}

// The original consumers validate an internal answer envelope. Only this native
// flat-file boundary translates their professional pointers and correction text.
// Keep the original error in the chain and never modify shared validator fields.
func nativeS9FlatArtifactError(kind, path string, err error) error {
	var original *workpacket.AnswerError
	if !errors.As(err, &original) || len(original.Fields) == 0 {
		return err
	}
	fields := append([]workpacket.FieldError(nil), original.Fields...)
	for i := range fields {
		if fields[i].Pointer == "/answer" {
			fields[i].Pointer = "/"
		} else if strings.HasPrefix(fields[i].Pointer, "/answer/") {
			fields[i].Pointer = strings.TrimPrefix(fields[i].Pointer, "/answer")
		} else {
			continue
		}
		fields[i].Repair = fmt.Sprintf("Correct %s in the private %s file %s using the original question and actual observations; keep professional fields at the top level.", fields[i].Pointer, kind, path)
	}
	return &nativeS9ArtifactFailure{original: err, fields: &workpacket.AnswerError{Label: "actual native " + kind + " at " + path, Fields: fields}}
}

type nativeS9ArtifactFailure struct {
	original error
	fields   *workpacket.AnswerError
}

func (e *nativeS9ArtifactFailure) Error() string { return e.fields.Error() }
func (e *nativeS9ArtifactFailure) Unwrap() error { return e.original }
func (e *nativeS9ArtifactFailure) As(target any) bool {
	if fields, ok := target.(**workpacket.AnswerError); ok {
		*fields = e.fields
		return true
	}
	return false
}
