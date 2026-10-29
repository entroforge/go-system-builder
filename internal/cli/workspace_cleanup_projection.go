package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func workspaceNativePrivateArea(ctx context.Context, root string, p workpacket.RepairResultPacket, e workspace.Execution) (integration.WorkerPrivateAreaSpec, error) {
	area, err := repair.NativeS9PrivateArea(ctx, root, p, e)
	if err != nil || area == "" {
		return integration.WorkerPrivateAreaSpec{}, err
	}
	var session repair.RepairSession
	if err := json.Unmarshal(p.Inputs.Sources.Session, &session); err != nil {
		return integration.WorkerPrivateAreaSpec{}, fmt.Errorf("read original frozen Session for Worker private area: %w", err)
	}
	if session.SessionID != p.Binding.Sources.SessionID || session.RuntimeID != e.RuntimeID || session.BaselineGeneration != e.BaselineGeneration {
		return integration.WorkerPrivateAreaSpec{}, fmt.Errorf("Worker private area original frozen Session differs")
	}
	baseline := make([]string, 0, len(session.BaselineArtifacts))
	for _, artifact := range session.BaselineArtifacts {
		baseline = append(baseline, artifact.Path)
	}
	return integration.WorkerPrivateAreaSpec{Path: area, BaseCommit: e.BaseCommit, WritePaths: append([]string(nil), e.WritePaths...), BaselinePaths: baseline}, nil
}

func workspaceIgnoredOriginalProjection(ctx context.Context, root, worker string, p workpacket.RepairResultPacket, c repair.WorkspaceCandidate) (map[string]string, error) {
	if c.Provenance == nil || c.Provenance.PacketID != p.ID {
		return nil, fmt.Errorf("Worker cleanup projection requires exact original Candidate")
	}
	out := map[string]string{}
	display := p.Draft
	display.Read = append([]workpacket.ReadItem(nil), p.Draft.Read...)
	for i := range display.Read {
		if !filepath.IsAbs(display.Read[i].Path) {
			display.Read[i].Path = filepath.Join(root, filepath.FromSlash(display.Read[i].Path))
		}
	}
	view, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		return nil, err
	}
	view = append(view, '\n')
	typed := p.Draft
	typed.Answer = append(json.RawMessage(nil), c.Provenance.SubmittedAnswer...)
	typedExpected, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		path     string
		expected []byte
		view     bool
	}{{p.ViewPath, view, true}, {p.AnswerPath, typedExpected, false}} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := workpacket.ReadFile(worker, item.path, 20<<20)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := workpacket.ValidateUniqueJSON(raw); err != nil {
			return nil, err
		}
		if item.view {
			if !bytes.Equal(raw, item.expected) {
				return nil, fmt.Errorf("Worker cleanup original issued view changed: %s", item.path)
			}
		} else {
			var actual, want any
			if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(item.expected, &want) != nil || !reflect.DeepEqual(actual, want) {
				return nil, fmt.Errorf("Worker cleanup typed DTO contains unproved content: %s", item.path)
			}
			answer, issues := workpacket.ValidateRepairResultAnswer(p, raw)
			if len(issues) > 0 || !bytes.Equal(answer, c.Provenance.SubmittedAnswer) {
				return nil, fmt.Errorf("Worker cleanup typed answer differs from original accepted Candidate: %s", item.path)
			}
		}
		out[item.path] = workpacket.Digest(raw)
	}
	return out, nil
}
