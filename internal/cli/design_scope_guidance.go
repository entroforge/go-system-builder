package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func stageDesignSourceDelivery(state map[string]any) string {
	if ref, err := fileview.DevelopmentRef(state); err == nil {
		return " Commit formal upstream design materials and any repaired sources to the bound development source " + ref + " before submitting or continuing the pending original outlet; S3 reads committed sources and checks disk agreement. An already received report stays received; do not refill its professional answer."
	} else {
		return " Bound development source is unavailable: " + err.Error() + ". Restore the existing binding; do not guess a branch."
	}
}

// stageDesignScopeContext exposes consumer-selected membership before the
// author submits. This is a disk inventory, not frozen-source validation;
// ordinary guidance must not rebuild the S3 closure or issue another packet.
func stageDesignScopeContext(root string, state map[string]any) string {
	var b strings.Builder
	b.WriteString("\nS2 architecture registration batch (architecture_path identifies the report document, not the entire batch):")
	batch, err := transition.DesignRegistrationBatch(root, state)
	if err != nil {
		fmt.Fprintf(&b, "\n- Batch inventory unavailable: %v. Repair this named source before relying on the batch listing.", err)
	}
	if len(batch) == 0 && err == nil {
		b.WriteString("\n- No declared architecture members yet; author the actual design using the supplied template.")
	}
	for _, doc := range batch {
		fmt.Fprintf(&b, "\n- %s — status=%s", filepath.Join(root, doc.Path), doc.Status)
		if doc.Status != "locked" {
			b.WriteString("; outlet gap: this batch member is not locked")
		}
		if doc.LegacyOwner {
			b.WriteString("; included by existing unlabelled-document compatibility")
		}
	}
	b.WriteString("\nComplete intended formal designs. Keep unadopted alternatives as discussion in the actual architecture/ADR, not as falsely locked formal designs; do not change ownership merely to evade this batch. This inventory is system-derived, not another answer field.")
	b.WriteString("\nS3 committed source scope: registered designs, every non-template module below docs/design/prototypes (recursively), current-REQ accepted ADRs, and their real conditional dependencies. Global source consumption does not ask you to redesign unrelated modules. Current disk module entry paths:")
	entries, err := os.ReadDir(filepath.Join(root, "docs/design/prototypes"))
	if os.IsNotExist(err) {
		b.WriteString("\n- No prototypes directory on disk; S3 proves its absence from the committed docs/design directory and checks the REQ's explicit no-scenario basis.")
	} else if err != nil {
		fmt.Fprintf(&b, "\n- Module inventory unavailable at %s: %v.", filepath.Join(root, "docs/design/prototypes"), err)
	} else if modules, err := runtime.ContractModulePaths(entries); err != nil {
		fmt.Fprintf(&b, "\n- Module inventory needs repair: %v.", err)
	} else {
		if len(modules) == 0 {
			b.WriteString("\n- No non-template module directories; S3 checks the REQ's explicit no-scenario basis.")
		}
		for _, module := range modules {
			fmt.Fprintf(&b, "\n- %s", filepath.Join(root, module))
		}
	}
	b.WriteString("\nThese are disk entry paths, not proof of committed or frozen content. The original S3 preparation checks the bound Git source and disk agreement; repair its named source error without deleting unrelated history or guessing questionnaire paths.")
	return b.String()
}
