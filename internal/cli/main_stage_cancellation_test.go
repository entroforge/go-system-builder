package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestMainStageReceptionCanceledBeforePendingPreservesOriginalPair(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			var root, packetID, answerPath string
			var save func(*submission.Service, context.Context, string) (submission.Receipt, error)
			switch stage {
			case "S2":
				r, p := newDesignFixture(t, "none")
				root, packetID, answerPath = r, p.ID, p.AnswerPath
				stageJSON(t, root, answerPath, designDraft(p))
				save = (*submission.Service).SaveDesign
			case "S3":
				r, p := newContractAnswerFixture(t)
				root, packetID, answerPath = r, p.ID, p.AnswerPath
				stageJSON(t, root, answerPath, completedContractDraft(p))
				save = (*submission.Service).SaveContracts
			case "S4":
				r, p := newTaskPlanningAnswerFixture(t)
				root, packetID, answerPath = r, p.ID, p.AnswerPath
				stageJSON(t, root, answerPath, completedTaskPlanningDraft(p))
				save = (*submission.Service).SaveTaskPlanning
			}
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			beforeState := mustStageFile(t, service.StatePath)
			beforeJournal := mustStageFile(t, service.JournalPath)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			original := service.Validator
			service.Validator = cancelValidatedReception{CandidateValidator: original, packetID: packetID, cancel: cancel}
			received, err := save(service, ctx, answerPath)
			if !errors.Is(err, context.Canceled) || received.Accepted || received.WorkComplete {
				t.Fatalf("canceled Main reception started a commit: %+v err=%v", received, err)
			}
			if !bytes.Equal(beforeState, mustStageFile(t, service.StatePath)) || !bytes.Equal(beforeJournal, mustStageFile(t, service.JournalPath)) {
				t.Fatal("cancellation changed the original Runtime pair")
			}
			if _, err := os.Stat(service.StatePath + ".commit-pending.json"); !os.IsNotExist(err) {
				t.Fatalf("cancellation published a pending commit: %v", err)
			}
			service.Validator = original
			if resumed, err := save(service, t.Context(), answerPath); err != nil || !resumed.Accepted {
				t.Fatalf("original unreceived answer could not resume: %+v err=%v", resumed, err)
			}
		})
	}
}
