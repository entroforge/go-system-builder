package runtime

import (
	"fmt"
)

// HumanReleaseDisposition is the explicit decision an operator makes when
// migrating a legacy S11 human-release gateway.
type HumanReleaseDisposition string

const (
	HumanReleaseDispositionApprove            HumanReleaseDisposition = "approve"
	HumanReleaseDispositionDefer              HumanReleaseDisposition = "defer"
	HumanReleaseDispositionRejectDefect       HumanReleaseDisposition = "reject_defect"
	HumanReleaseDispositionRejectAcceptance   HumanReleaseDisposition = "reject_acceptance"
	HumanReleaseDispositionRejectReleaseAudit HumanReleaseDisposition = "reject_release_audit"
	HumanReleaseDispositionAbort              HumanReleaseDisposition = "abort"
)

// HumanReleaseTransitionID maps an explicit S11 disposition to its fixed
// transition identifier. It intentionally accepts no target state: callers
// must provide one of the finite human decision dispositions.
func HumanReleaseTransitionID(disposition string) (string, error) {
	switch HumanReleaseDisposition(disposition) {
	case HumanReleaseDispositionApprove:
		return "TR-025", nil
	case HumanReleaseDispositionDefer:
		return "TR-026", nil
	case HumanReleaseDispositionRejectDefect:
		return "TR-027", nil
	case HumanReleaseDispositionRejectAcceptance:
		return "TR-028", nil
	case HumanReleaseDispositionRejectReleaseAudit:
		return "TR-029", nil
	case HumanReleaseDispositionAbort:
		return "TR-030", nil
	default:
		return "", fmt.Errorf("unknown human release disposition %q", disposition)
	}
}

func isRolloverTerminalState(state string) bool {
	return state == "release_authorized" || state == "aborted"
}
