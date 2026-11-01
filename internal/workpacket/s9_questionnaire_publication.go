package workpacket

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// PublishS9Questionnaire writes a new system-owned draft at an unoccupied
// location. The caller records the returned actual path in its packet Writer.
func PublishS9Questionnaire(ctx context.Context, root, preferred string, data []byte, limit int64) (string, error) {
	return PublishS9QuestionnairePrepared(ctx, root, preferred, func(string) []byte { return data }, limit)
}

// PublishS9QuestionnairePrepared renders self-references after the candidate
// location is chosen and records only an exclusively created file.
func PublishS9QuestionnairePrepared(ctx context.Context, root, preferred string, prepare func(string) []byte, limit int64) (string, error) {
	parent, _, err := OpenParent(root, ".claude/s9-questionnaire-root-probe", true)
	if err != nil {
		return "", err
	}
	if err := parent.Close(); err != nil {
		return "", err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := preferred
		if attempt > 0 {
			parts := strings.SplitN(strings.TrimPrefix(preferred, ".claude/"), "/", 2)
			if len(parts) != 2 {
				return "", fmt.Errorf("invalid S9 questionnaire path")
			}
			candidate = path.Join(".claude", fmt.Sprintf("%s-alt-%d", parts[0], attempt), parts[1])
		}
		created, err := PublishNewBounded(root, candidate, prepare(candidate), limit)
		if err != nil {
			return "", err
		}
		if created {
			return candidate, nil
		}
	}
}
