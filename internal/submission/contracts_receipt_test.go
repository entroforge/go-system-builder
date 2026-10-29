package submission

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractUnreceivedOutputErrorKeepsCurrentAnswer(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	p := workpacket.ContractPacket{AnswerPath: ".claude/submissions/current/answer.json"}
	snapshot := runtime.Snapshot{State: map[string]any{"lifecycle": map[string]any{"state": "planning", "phase": "contracts"}}}
	r := s.unreceivedContractReceipt(snapshot, p, fmt.Errorf("S3 artifact checkout differs from source view: docs/dev/contracts/BE-001.md"))
	if r.Accepted || r.WorkComplete || !strings.Contains(r.NextAction, filepath.Join(root, filepath.FromSlash(p.AnswerPath))) || !strings.Contains(r.NextAction, "尚未接收") || strings.Contains(r.NextAction, "具名修订") || !strings.Contains(r.Error, "BE-001.md") {
		t.Fatalf("unreceived S3 output error suggests a different submission/revision: %+v", r)
	}
}
