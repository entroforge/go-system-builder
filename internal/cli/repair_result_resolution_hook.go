package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func isS9RepairResultResolutionPath(path string) bool {
	clean := "/" + strings.Trim(filepath.ToSlash(filepath.Clean(path)), "/") + "/"
	return strings.Contains(clean, "/.claude/submissions/") && strings.Contains(clean, "/S9-result/") && strings.HasSuffix(clean, "/main-resolution.json/")
}

func saveRepairResultResolutionHook(root string, request policy.Input, path string, stdout, stderr io.Writer) (bool, int) {
	reject := func(err error) (bool, int) {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: err.Error()}, "S9")
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	reader, err := submission.NewReadOnly(root)
	if err != nil {
		return reject(err)
	}
	packet, found, err := reader.RepairResultResolutionForPath(path)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("S9 Main resolution save path has no exact original packet")
		}
		return reject(err)
	}
	service, err := submission.New(root)
	if err != nil {
		return reject(err)
	}
	receipt, saveErr := service.SaveRepairResultResolutionFrom(ctx, path, nil)
	if saveErr != nil {
		fmt.Fprintf(stderr, "S9 Main resolution not accepted: %v\n", saveErr)
		if receipt.Error == "" {
			receipt.Error = saveErr.Error()
		}
	}
	if receipt.PacketID == "" {
		receipt.PacketID = packet.ID
	}
	return true, renderStageAnswerHook(stdout, path, receipt, "S9")
}
