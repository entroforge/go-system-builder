package submission

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type authoringSource struct {
	Root, Path string
	Execution  *workspace.Execution
}

const s5DocumentReviewTemplateID = "s5-document-review-v1"

// knownNonS5WorkPacketSubtype recognizes the other packet types registered in
// the Runtime. S5 consumers skip only these known typed domains; unknown or
// malformed rows must still reach FindWorkPacket and fail closed.
func knownNonS5WorkPacketSubtype(templateID any) bool {
	value, ok := templateID.(string)
	if !ok {
		return false
	}
	switch value {
	case s5DocumentReviewTemplateID:
		return false
	case "s2-design-v1", "s3-contract-v1", "s4-task-planning-v1",
		"s7-review-result-v1", "s8-hypothesis-result-v1",
		"s9-plan-report-v1", "s9-repair-result-v1":
		return true
	default:
		return false
	}
}

func shouldSkipKnownNonS5WorkPacketRow(row map[string]any) bool {
	packetID, ok := row["packet_id"].(string)
	return ok && packetID != "" && knownNonS5WorkPacketSubtype(row["template_id"])
}

// skipKnownNonS5WorkPacket routes a submission by its one registered packet
// row. Duplicate identities are not skipped; the strict typed lookup below
// remains responsible for rejecting them.
func skipKnownNonS5WorkPacket(state map[string]any, packetID string) bool {
	if packetID == "" {
		return false
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var matched map[string]any
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rowPacketID, ok := row["packet_id"].(string)
		if !ok || rowPacketID != packetID {
			continue
		}
		if matched != nil {
			return false
		}
		matched = row
	}
	return matched != nil && shouldSkipKnownNonS5WorkPacketRow(matched)
}

func workerAnswerAuthoringPath(state map[string]any, e workspace.Execution) (string, error) {
	b, err := workspace.Decode(state)
	if err != nil || b == nil {
		return "", fmt.Errorf("Worker registry unavailable: %v", err)
	}
	paths, err := b.WorkerSubmissionPaths(e)
	if err != nil {
		return "", err
	}
	return paths.Answer, nil
}

func (s *Service) source(ctx context.Context, state map[string]any, owner, path string) (workpacket.Packet, authoringSource, error) {
	s = s.WithContext(ctx)
	if !runtime.PacketEnabled(state) || owner == "" {
		return workpacket.Packet{}, authoringSource{}, fmt.Errorf("saved answer has no enabled registered owner")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	path = filepath.Clean(path)
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && shouldSkipKnownNonS5WorkPacketRow(row) {
			continue
		}
		p, ok, err := runtime.FindWorkPacket(state, text(row["packet_id"]))
		if err != nil || !ok {
			return p, authoringSource{}, fmt.Errorf("issued packet cannot be read: %v", err)
		}
		// Never turn an old received source into a new delivery after revision.
		if r, accepted, err := runtime.FindSubmission(state, p.ID); err != nil {
			return p, authoringSource{}, err
		} else if accepted && r.RevisionPacketID != "" {
			if path == filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath)) {
				return p, authoringSource{}, runtime.ErrSubmissionSuperseded
			}
			continue
		}
		e, declared, err := workspace.ReviewExecution(state, p)
		if err != nil {
			return p, authoringSource{}, err
		}
		mainPath := filepath.Join(s.Root, filepath.FromSlash(p.AnswerPath))
		workerAnswerPath := ""
		workerPath := ""
		if declared {
			workerAnswerPath, err = workerAnswerAuthoringPath(state, e)
			if err != nil {
				return p, authoringSource{}, err
			}
			workerPath = filepath.Join(e.Path, filepath.FromSlash(workerAnswerPath))
		}
		if path != mainPath && (!declared || path != workerPath) {
			continue
		}
		if p.Binding.Owner != owner {
			return p, authoringSource{}, fmt.Errorf("answer belongs to another registered owner")
		}
		if err != nil {
			return p, authoringSource{}, err
		}
		if path == mainPath {
			if declared {
				return p, authoringSource{}, fmt.Errorf("review has an independent execution; shared Main authoring is retired")
			}
			source := authoringSource{Root: s.Root, Path: p.AnswerPath}
			return p, source, workpacket.CheckPath(source.Root, source.Path)
		}
		source := authoringSource{Root: e.Path, Path: workerAnswerPath, Execution: &e}
		if err := s.validateSource(ctx, state, p, source); err != nil {
			return p, source, err
		}
		return p, source, nil
	}
	return workpacket.Packet{}, authoringSource{}, fmt.Errorf("answer path was not issued for a current execution")
}

func (s *Service) validateSource(ctx context.Context, state map[string]any, p workpacket.Packet, source authoringSource) error {
	s = s.WithContext(ctx)
	e, declared, err := workspace.ReviewExecution(state, p)
	if err != nil {
		return err
	}
	if source.Execution == nil {
		if declared || source.Root != s.Root || source.Path != p.AnswerPath {
			return fmt.Errorf("shared Main answer source was replaced")
		}
		return workpacket.CheckPath(source.Root, source.Path)
	}
	_, accepted, err := runtime.FindSubmission(state, p.ID)
	if err != nil {
		return err
	}
	// A retired execution can identify an already received answer for exact
	// replay. It never provides a source for a new domain reception.
	usable := e.Status == "ready" || accepted && e.Status == "retired"
	if !declared || !usable || e.Generation != source.Execution.Generation || e.Path != source.Root || e.AgentID != p.Binding.Owner || e.BootstrapSHA256 == "" {
		return fmt.Errorf("Worker answer source is stale, replaced or not ready")
	}
	b, err := workspace.Decode(state)
	if err != nil || b == nil {
		return fmt.Errorf("Worker registry unavailable: %v", err)
	}
	if err := b.ValidateAuthority(state); err != nil {
		return err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return err
	}
	if err := b.ValidateDocumentReviewView(e, p); err != nil {
		return err
	}
	if err := workpacket.CheckPath(source.Root, source.Path); err != nil {
		return err
	}
	if !accepted {
		life, _ := state["lifecycle"].(map[string]any)
		if life["state"] != "document_verification" || number(life["phase_revision"]) != p.Binding.StageEntry {
			return fmt.Errorf("Worker's S5 origin was replaced")
		}
		if err := b.ValidateInputs(e); err != nil {
			return err
		}
	}
	return nil
}
