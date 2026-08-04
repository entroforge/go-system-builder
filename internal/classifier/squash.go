package classifier

// IsSquashMerge reports whether command is a tokenized git squash merge.
// Unknown or unparseable commands are not treated as squash merges.
func IsSquashMerge(command string) bool {
	_, matched := ParseSquashMerge(command)
	return matched
}

// ParseSquashMerge returns a canonical squash-merge command when tokenized
// resolution proves one of the retained git or GitHub CLI shapes.
func ParseSquashMerge(command string) (string, bool) {
	resolved, err := Resolve(command)
	if err != nil {
		return "", false
	}
	if !hasAnyFlag(resolved.Args, []string{"--squash"}) {
		return "", false
	}
	switch {
	case resolved.Family == "git" && resolved.Subcommand == "merge":
		return "git merge --squash", true
	case resolved.Family == "gh" && resolved.Subcommand == "pr merge":
		return "gh pr merge --squash", true
	default:
		return "", false
	}
}
