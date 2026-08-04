// Package classifier: resolve.go implements the per-program resolvers that
// convert a tokenized bash command into a ResolvedCommand describing which
// paths the command will mutate and whether it is a protected-release shape
// (BUG-002 §4b.2(b) and §4b.2(c)). The protected-commands table is loaded
// from docs/release_audits/protected_commands.json via LoadProtectedCommands
// (BUG-002 §4b.2(d) lines 245-250) and matched against the resolved program +
// subcommand + flags.
//
// Resolvers implemented:
//   - git        (with -C <dir>, --git-dir, --work-tree, alias resolution)
//   - sed        (-i / -i.bak / --in-place → Mutates; positional file args)
//   - awk        (-i inplace → Mutates)
//   - perl       (-i / -pi / -pi.bak → Mutates)
//   - ed         (positional file args → Mutates)
//   - gh         (release {create,edit,delete,upload}, pr merge)
//   - npm        (publish)
//   - goreleaser (release)
//   - terraform  (apply, destroy)
//   - kubectl    (apply, delete)
//   - aws        (s3 cp, s3 sync)
//   - go         (read-only test, build, vet)
//
// Default behaviour for an unknown program invoked by an activated subagent
// is "deny" (BUG-002 §4b.2(b) line 178).
package classifier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ProtectedCommand is one row of the protected-commands table loaded from
// docs/release_audits/protected_commands.json. The table is data-driven so
// adding a new release channel requires only a JSON change (BUG-002 §4b.2(d)).
type ProtectedCommand struct {
	Family       string        `json:"family"`
	Subcommand   string        `json:"subcommand,omitempty"`
	SubArgsAny   []string      `json:"subargs_any,omitempty"`
	FlagsAny     []string      `json:"flags_any,omitempty"`
	ArgsPatterns []ArgsPattern `json:"args_patterns,omitempty"`
	Reason       string        `json:"reason"`
}

// ArgsPattern is either a literal string or a regular expression (BUG-002
// §4b.2(d) lines 230-242). The discriminator is presence of "regex".
type ArgsPattern struct {
	Literal string `json:"literal,omitempty"`
	Regex   string `json:"regex,omitempty"`
}

// matchLiteral reports whether args contains literal as an exact argv element.
func (p ArgsPattern) matchLiteral(args []string) bool {
	if p.Literal == "" {
		return false
	}
	for _, a := range args {
		if a == p.Literal {
			return true
		}
	}
	return false
}

// matchRegex reports whether args contains any element matching the regex.
func (p ArgsPattern) matchRegex(args []string) (bool, error) {
	if p.Regex == "" {
		return false, nil
	}
	re, err := regexp.Compile(p.Regex)
	if err != nil {
		return false, fmt.Errorf("compile regex %q: %w", p.Regex, err)
	}
	for _, a := range args {
		if re.MatchString(a) {
			return true, nil
		}
	}
	return false, nil
}

// ResolvedCommand is the structured view produced by Resolve. The policy
// engine uses AffectedPaths to gate HS-004 (bound REQ writes) against Bash
// commands and uses IsProtectedRelease to gate HS-005 (human release).
type ResolvedCommand struct {
	Program            string
	Subcommand         string
	Args               []Token
	AffectedPaths      []string
	Mutates            bool
	IsProtectedRelease bool
	// Family is the canonical resolver key (lowercase). For resolved
	// commands whose program is not in the resolver table, Family is "".
	Family string
}

// LoadProtectedCommands reads the data-driven protected-commands table from
// docs/release_audits/protected_commands.json (BUG-002 §4b.2(d) lines 245-250).
// The returned slice is ordered as it appears in the JSON file so tests can
// reference rows by index when needed.
func LoadProtectedCommands(root string) ([]ProtectedCommand, error) {
	path := filepath.Join(root, "docs", "release_audits", "protected_commands.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read protected_commands.json: %w", err)
	}
	var rows []ProtectedCommand
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode protected_commands.json: %w", err)
	}
	return rows, nil
}

// Resolve tokenizes the command (returning an error on unbalanced shell
// input) and dispatches to the appropriate per-family resolver. The Program
// field is always lowercased; Subcommand preserves the literal case so the
// policy engine can compare against protected-commands rows verbatim.
func Resolve(command string) (ResolvedCommand, error) {
	tokens, err := Tokenize(command)
	if err != nil {
		return ResolvedCommand{}, err
	}
	r := ResolvedCommand{Program: FirstWord(tokens), Args: tokens}
	switch r.Program {
	case "git":
		resolveGit(&r)
	case "sed":
		resolveSed(&r)
	case "awk":
		resolveAwk(&r)
	case "perl":
		resolvePerl(&r)
	case "ed":
		resolveEd(&r)
	case "gh":
		resolveGH(&r)
	case "npm":
		resolveNPM(&r)
	case "goreleaser":
		resolveGoReleaser(&r)
	case "terraform":
		resolveTerraform(&r)
	case "kubectl":
		resolveKubectl(&r)
	case "aws":
		resolveAWS(&r)
	case "go":
		resolveGo(&r)
	default:
		r.Family = ""
	}
	return r, nil
}

// MatchProtectedCommands reports whether the resolved command matches any
// row in the supplied protected-commands table. The matcher implements the
// shape described in BUG-002 §4b.2(d) lines 230-242: family match, subcommand
// match, args_any literal / regex, flags_any.
func MatchProtectedCommands(r ResolvedCommand, table []ProtectedCommand) (bool, string, error) {
	for _, row := range table {
		if row.Family != r.Program {
			continue
		}
		if row.Subcommand != "" && row.Subcommand != r.Subcommand {
			continue
		}
		if len(row.FlagsAny) > 0 {
			if !hasAnyFlag(r.Args, row.FlagsAny) {
				continue
			}
		}
		if len(row.SubArgsAny) > 0 {
			if !hasAnyArg(r.Args, row.SubArgsAny) {
				continue
			}
		}
		if len(row.ArgsPatterns) > 0 {
			argStrs := argStrings(r.Args)
			matched := false
			for _, p := range row.ArgsPatterns {
				if p.Literal != "" {
					if p.matchLiteral(argStrs) {
						matched = true
						break
					}
				} else if p.Regex != "" {
					ok, err := p.matchRegex(argStrs)
					if err != nil {
						return false, "", err
					}
					if ok {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		return true, row.Reason, nil
	}
	return false, "", nil
}

// hasAnyFlag reports whether tokens contains any of the supplied flags as a
// flag-kind token (TkShortFlag or TkLongFlag) with matching value.
func hasAnyFlag(tokens []Token, flags []string) bool {
	for _, t := range tokens {
		if t.Kind != TkShortFlag && t.Kind != TkLongFlag {
			continue
		}
		for _, f := range flags {
			if t.Value == f {
				return true
			}
		}
	}
	return false
}

// hasAnyArg reports whether tokens contains any of the supplied argv
// elements as a word/value token.
func hasAnyArg(tokens []Token, args []string) bool {
	words := argStrings(tokens)
	for _, target := range args {
		for _, w := range words {
			if w == target {
				return true
			}
		}
	}
	return false
}

// argStrings returns the literal argv elements from the token stream.
func argStrings(tokens []Token) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		switch t.Kind {
		case TkWord, TkShortFlag, TkLongFlag, TkValue, TkQuotedString:
			out = append(out, t.Value)
		}
	}
	return out
}

// --- Per-family resolvers ----------------------------------------------------

func resolveGit(r *ResolvedCommand) {
	r.Family = "git"
	r.Subcommand = findSubcommand(r.Args)
	// Extract -C <dir>, --git-dir=<dir>, --work-tree=<dir>. We currently do
	// not surface the resolved directory to callers; it would change only
	// affected-paths glob expansion which is left to a future revision.
	for idx, t := range r.Args {
		_ = idx
		switch {
		case t.Kind == TkLongFlag && strings.HasPrefix(t.Value, "--git-dir="):
			// ditto
		case t.Kind == TkLongFlag && strings.HasPrefix(t.Value, "--work-tree="):
			// ditto
		}
	}
	switch r.Subcommand {
	case "checkout":
		// `git checkout <branch> -- <files>` mutates the files after `--`.
		files, ok := splitAfterDoubleDash(r.Args)
		if ok {
			r.Mutates = true
			if len(files) > 0 {
				r.AffectedPaths = append(r.AffectedPaths, files...)
			}
		}
	case "restore":
		// `git restore <files...>` — file args.
		files, ok := splitAfterDoubleDash(r.Args)
		if ok {
			r.Mutates = true
			r.AffectedPaths = append(r.AffectedPaths, files...)
		} else {
			for _, f := range positionalAfterSubcommand(r.Args, "restore", true) {
				r.AffectedPaths = append(r.AffectedPaths, f)
			}
			if len(r.AffectedPaths) > 0 {
				r.Mutates = true
			}
		}
	case "apply":
		// `git apply <patch>` — patch file args.
		for _, f := range positionalAfterSubcommand(r.Args, "apply", true) {
			r.AffectedPaths = append(r.AffectedPaths, f)
		}
		if len(r.AffectedPaths) > 0 {
			r.Mutates = true
		}
	case "tag":
		r.Mutates = true
	case "merge":
		r.Mutates = true
	case "push":
		r.Mutates = true
	case "commit":
		r.Mutates = true
	}
}

func resolveSed(r *ResolvedCommand) {
	r.Family = "sed"
	r.Subcommand = ""
	if hasAnyFlag(r.Args, []string{"-i", "--in-place"}) {
		r.Mutates = true
	}
	// Positional file args come after the script and any flag value.
	scriptConsumed := false
	flagValueConsumed := false
	for _, t := range r.Args {
		switch t.Kind {
		case TkWord:
			if r.Program == "" && t.Value == "sed" {
				continue
			}
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		case TkShortFlag, TkLongFlag:
			if t.Value == "-i" || t.Value == "--in-place" {
				continue
			}
			flagValueConsumed = true
		case TkValue:
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			if flagValueConsumed {
				flagValueConsumed = false
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		}
	}
}

func resolveAwk(r *ResolvedCommand) {
	r.Family = "awk"
	if hasAnyFlag(r.Args, []string{"-i", "--include"}) {
		// awk -i inplace or --include=inplace
		r.Mutates = true
	}
	// Positional file args come after the program.
	scriptConsumed := false
	for _, t := range r.Args {
		switch t.Kind {
		case TkWord:
			if t.Value == "awk" {
				continue
			}
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		case TkValue:
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		}
	}
}

func resolvePerl(r *ResolvedCommand) {
	r.Family = "perl"
	if hasAnyFlag(r.Args, []string{"-i", "-pi", "-pi.bak", "-pli", "-pl", "-p", "-n"}) {
		r.Mutates = true
	}
	// Detect combined short flags like -pi, -pi.bak (Tokenize emits them as
	// TkShortFlag with the combined value).
	for _, t := range r.Args {
		if t.Kind == TkShortFlag && (strings.HasPrefix(t.Value, "-pi") || strings.HasPrefix(t.Value, "-pl") || strings.HasPrefix(t.Value, "-p") || strings.HasPrefix(t.Value, "-n") || strings.HasPrefix(t.Value, "-i")) {
			r.Mutates = true
		}
	}
	// Walk the tokens; flags consume their following value (TkValue or
	// TkQuotedString) as the script. The remaining TkWord tokens are file
	// args (or further positional).
	scriptConsumed := false
	for i, t := range r.Args {
		switch t.Kind {
		case TkShortFlag, TkLongFlag:
			if t.Value == "perl" {
				continue
			}
			// Skip the flag's value if it immediately follows (e.g. -e SCRIPT).
			if i+1 < len(r.Args) {
				next := r.Args[i+1]
				if next.Kind == TkValue || next.Kind == TkQuotedString {
					if !scriptConsumed {
						scriptConsumed = true
					}
					i++
					continue
				}
			}
			if !scriptConsumed {
				// Combined flag like -pi — script is inline; nothing more to consume.
				scriptConsumed = true
			}
		case TkWord:
			if t.Value == "perl" {
				continue
			}
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		case TkValue, TkQuotedString:
			if !scriptConsumed {
				scriptConsumed = true
				continue
			}
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		}
	}
}

func resolveEd(r *ResolvedCommand) {
	r.Family = "ed"
	// `ed <file>` edits the file in place by default.
	positionalCount := 0
	for _, t := range r.Args {
		switch t.Kind {
		case TkWord:
			if t.Value == "ed" {
				continue
			}
			positionalCount++
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		case TkValue:
			positionalCount++
			r.AffectedPaths = append(r.AffectedPaths, t.Value)
		}
	}
	if positionalCount > 0 {
		r.Mutates = true
	}
}

func resolveGH(r *ResolvedCommand) {
	r.Family = "gh"
	// Build the subcommand from the first two positional tokens after the
	// program name (`gh release create`, `gh pr merge`). We bypass the
	// generic findSubcommand/findPositionalAfterIndex helpers because gh's
	// verb can be confused with version-like trailing args (e.g. `gh
	// release create v1.0.0` — "v1.0.0" must NOT become the sub-subcommand).
	seenProgram := false
	positionals := []string{}
	for _, t := range r.Args {
		if !seenProgram {
			if t.Kind == TkWord && t.Value == "gh" {
				seenProgram = true
			}
			continue
		}
		if t.Kind == TkWord {
			positionals = append(positionals, t.Value)
		}
	}
	if len(positionals) >= 1 {
		r.Subcommand = positionals[0]
	}
	if len(positionals) >= 2 {
		r.Subcommand = r.Subcommand + " " + positionals[1]
	}
	switch {
	case strings.HasPrefix(r.Subcommand, "release "):
		switch positionals[1] {
		case "create", "edit", "delete", "upload":
			r.Mutates = true
		}
	case r.Subcommand == "pr merge":
		r.Mutates = true
	}
}

func resolveNPM(r *ResolvedCommand) {
	r.Family = "npm"
	r.Subcommand = findSubcommand(r.Args)
	if r.Subcommand == "publish" {
		r.Mutates = true
	}
}

func resolveGoReleaser(r *ResolvedCommand) {
	r.Family = "goreleaser"
	r.Subcommand = findSubcommand(r.Args)
	if r.Subcommand == "release" {
		r.Mutates = true
	}
}

func resolveTerraform(r *ResolvedCommand) {
	r.Family = "terraform"
	r.Subcommand = findSubcommand(r.Args)
	switch r.Subcommand {
	case "apply", "destroy":
		r.Mutates = true
	}
}

func resolveKubectl(r *ResolvedCommand) {
	r.Family = "kubectl"
	r.Subcommand = findSubcommand(r.Args)
	switch r.Subcommand {
	case "apply", "delete":
		r.Mutates = true
	}
}

func resolveAWS(r *ResolvedCommand) {
	r.Family = "aws"
	// Collect the first two positional words (service, verb) so we can
	// emit a multi-word subcommand like "s3 cp" / "s3 sync" that the
	// protected-commands table can match directly.
	parts := []string{}
	seenProgram := false
	for _, t := range r.Args {
		if !seenProgram {
			if t.Kind == TkWord && t.Value == "aws" {
				seenProgram = true
			}
			continue
		}
		if t.Kind == TkWord {
			parts = append(parts, t.Value)
			if len(parts) == 2 {
				break
			}
		}
	}
	if len(parts) >= 2 {
		service := parts[0]
		verb := parts[1]
		r.Subcommand = service + " " + verb
		if service == "s3" && (verb == "cp" || verb == "sync") {
			r.Mutates = true
		}
	} else if len(parts) == 1 {
		r.Subcommand = parts[0]
	}
}

func resolveGo(r *ResolvedCommand) {
	r.Family = "go"
	r.Subcommand = findSubcommand(r.Args)
	switch r.Subcommand {
	case "test", "build", "vet", "mod":
		// go test/build/vet/mod are classified read_only for HS-003.
	}
}

// --- Helper utilities -------------------------------------------------------

// findSubcommand returns the first TkWord token value that follows the
// program name, skipping flags and any flag-value token that immediately
// follows. Long flags whose value is embedded with `=` (`--git-dir=/tmp/x`)
// are recognised as self-contained flag tokens and do NOT consume the next
// TkWord as a flag-value. Returns "" if there is no subcommand.
func findSubcommand(tokens []Token) string {
	seenProgram := false
	skipNextWord := false
	for _, t := range tokens {
		if !seenProgram {
			if t.Kind == TkWord {
				seenProgram = true
			}
			continue
		}
		switch t.Kind {
		case TkShortFlag:
			// Short flags like `-C` always take a separate TkWord value.
			skipNextWord = true
		case TkLongFlag:
			// Long flags with `=` embed their value, so do NOT skip.
			if !strings.Contains(t.Value, "=") {
				skipNextWord = true
			}
		case TkWord:
			if skipNextWord {
				skipNextWord = false
				continue
			}
			return t.Value
		}
	}
	return ""
}

// findPositionalAfterIndex returns the i-th positional TkWord token (0-indexed
// from the program name, skipping flags and their immediate TkWord/TkValue
// values). Long flags with `=` embed their value, so do NOT consume the next
// TkWord. Returns "" if there are fewer positionals than requested.
func findPositionalAfterIndex(tokens []Token, index int) string {
	count := 0
	seenProgram := false
	skipNextWord := false
	for _, t := range tokens {
		if !seenProgram {
			if t.Kind == TkWord {
				seenProgram = true
			}
			continue
		}
		switch t.Kind {
		case TkShortFlag:
			skipNextWord = true
			continue
		case TkLongFlag:
			if !strings.Contains(t.Value, "=") {
				skipNextWord = true
			}
			continue
		case TkValue, TkQuotedString:
			continue
		case TkWord:
			if skipNextWord {
				skipNextWord = false
				continue
			}
			count++
			if count == index+1 {
				return t.Value
			}
		}
	}
	return ""
}

// splitAfterDoubleDash returns the file args that appear after the literal
// `--` separator. Returns (nil, false) when `--` is absent.
func splitAfterDoubleDash(tokens []Token) ([]string, bool) {
	seen := false
	for i, t := range tokens {
		if !seen {
			if t.Kind == TkWord && t.Value == "--" {
				seen = true
			}
			continue
		}
		// After `--`, every word/value is a file.
		switch t.Kind {
		case TkWord, TkValue:
			files := []string{}
			for _, rest := range tokens[i:] {
				switch rest.Kind {
				case TkWord, TkValue:
					files = append(files, rest.Value)
				}
			}
			return files, true
		}
	}
	return nil, false
}

// positionalAfterSubcommand returns the argv elements that follow the named
// subcommand, skipping flags and their values. The skipFirstArg flag, when
// true, also skips the first non-flag element (used when subcommand and a
// value-of-flag are positional).
func positionalAfterSubcommand(tokens []Token, subcommand string, skipFirstArg bool) []string {
	out := []string{}
	seenSub := false
	for i, t := range tokens {
		if !seenSub {
			if t.Kind == TkWord && t.Value == subcommand {
				seenSub = true
			}
			continue
		}
		switch t.Kind {
		case TkWord, TkValue:
			out = append(out, t.Value)
		case TkShortFlag, TkLongFlag:
			// Skip a flag's value if it is separate (e.g. `-C dir`).
			if i+1 < len(tokens) {
				next := tokens[i+1]
				if next.Kind == TkValue {
					i++
				}
			}
		}
	}
	if skipFirstArg && len(out) > 0 {
		out = out[1:]
	}
	return out
}
