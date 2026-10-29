package cli

import (
	"context"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func stageRepairResultQuestionnaireContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if !runtime.PacketEnabled(snapshot.State) || lifecycle["state"] != "bug_resolution" || lifecycle["phase"] != "fixing" {
		return ""
	}
	return s9ResultQuestionnaireContext(ctx, root, request, service, snapshot, owner)
}
