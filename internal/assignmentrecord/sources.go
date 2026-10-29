package assignmentrecord

import (
	"fmt"
	"strings"
)

// VerifySources checks the exact frozen bytes in the caller's original file
// view. Embedded CheckProfiles are covered by the manifest digest; their
// dependencies and fixtures have separate source rows and are checked here.
func VerifySources(record Record, read func(string) ([]byte, error)) error {
	if err := ValidateRecord(record); err != nil {
		return err
	}
	for _, source := range record.ContextSources {
		if source.Kind == "check_profile" {
			if !strings.HasPrefix(source.Ref, record.Manifest.Ref+"#assignments/"+record.AssignmentID+"/check_profiles/") {
				return fmt.Errorf("CheckProfile is outside Assignment manifest")
			}
			continue
		}
		data, err := read(source.Ref)
		if err != nil {
			return fmt.Errorf("Assignment source %s: %w", source.Ref, err)
		}
		if digest(data) != source.SHA256 {
			return fmt.Errorf("Assignment source %s changed after registration", source.Ref)
		}
	}
	return nil
}
