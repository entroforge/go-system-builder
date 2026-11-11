package checkexec

import (
	"path/filepath"
)

// PermissionObservation describes the actual original isolated runner. Its
// availability is measured by that runner, never selected by the receipt author.
type PermissionObservation struct {
	Version    string `json:"version"`
	Runner     string `json:"runner"`
	Platform   string `json:"platform"`
	Kernel     string `json:"kernel"`
	UID        int    `json:"uid"`
	GID        int    `json:"gid"`
	Groups     []int  `json:"groups"`
	MainRoot   string `json:"main_root"`
	CommonDir  string `json:"common_dir"`
	WorkerRoot string `json:"worker_root"`
	CWD        string `json:"cwd"`
	Available  bool   `json:"available"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

func (p PermissionObservation) Known() bool {
	if p.Version != "1.0.0" || p.Runner != "worker_bubblewrap_seccomp_v1" || !p.Available || p.Platform == "" || p.Kernel == "" || p.Groups == nil || p.UID < 0 || p.GID < 0 || p.Diagnostic != "" {
		return false
	}

	for _, path := range []string{p.MainRoot, p.CommonDir, p.WorkerRoot, p.CWD} {
		if !filepath.IsAbs(path) {
			return false
		}
	}
	return p.CWD == p.WorkerRoot
}

// ClosedStaticCheck is deliberately small: the original runner can read the
// host filesystem, so a whole repository digest cannot close arbitrary shell.
func ClosedStaticCheck(command string) bool {
	args, ok := LiteralArguments(command)
	if !ok {
		return false
	}
	if len(args) == 1 {
		return args[0] == "true" || args[0] == "false"
	}
	if len(args) != 3 || args[0] != "test" || (args[1] != "-f" && args[1] != "-s") {
		return false
	}
	return safeProfilePath(args[2])
}
