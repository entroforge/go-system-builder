package classifier

import (
	"path/filepath"
	"slices"
	"strings"
)

type Effect string

const (
	ProvenRead     Effect = "proven_read"
	KnownWrites    Effect = "known_writes"
	UnknownEffects Effect = "unknown_effects"
)

type CommandEffects struct {
	Effect  Effect   `json:"effect"`
	Paths   []string `json:"paths"`
	Segment []string `json:"segment,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// AnalyzeEffects uses the shared shell tokenizer and evaluates each segment.
// It never infers an interpreter's write surface from its script location.
func AnalyzeEffects(command string) CommandEffects {
	tokens, err := Tokenize(command)
	if err != nil {
		return CommandEffects{Effect: UnknownEffects, Reason: err.Error()}
	}
	result := CommandEffects{Effect: ProvenRead, Paths: []string{}}
	var argv []string
	unknown := func(reason string) CommandEffects {
		return CommandEffects{Effect: UnknownEffects, Paths: result.Paths, Segment: append([]string(nil), argv...), Reason: reason}
	}
	flush := func() bool {
		part := simpleEffects(argv)
		if part.Effect == UnknownEffects {
			result.Effect = UnknownEffects
			result.Segment = append([]string(nil), argv...)
			result.Reason = part.Reason
			return false
		}
		if part.Effect == KnownWrites {
			result.Effect = KnownWrites
			result.Paths = append(result.Paths, part.Paths...)
		}
		argv = nil
		return true
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.Expanded {
			return unknown("shell-expanded argv requires an enforced execution profile")
		}
		switch token.Kind {
		case TkPipe, TkAnd, TkOr, TkSemicolon:
			if !flush() {
				return result
			}
		case TkSubshell, TkBacktick, TkLParen, TkRParen:
			return unknown("shell substitution or compound scope requires an enforced execution profile")
		case TkRedirect:
			if token.Value != ">" && token.Value != ">>" && token.Value != "<" {
				return unknown("heredoc or dynamic file descriptor redirection")
			}
			if i+1 >= len(tokens) {
				return unknown("redirection has no target")
			}
			i++
			target := tokens[i]
			if target.Kind != TkWord && target.Kind != TkQuotedString && target.Kind != TkValue {
				return unknown("dynamic redirection target")
			}
			if target.Expanded || dynamicPath(target.Value) {
				return unknown("expanded redirection target")
			}
			if token.Value != "<" && target.Value != "/dev/null" {
				result.Effect = KnownWrites
				result.Paths = append(result.Paths, target.Value)
			}
		default:
			argv = append(argv, token.Value)
		}
	}
	flush()
	result.Paths = slices.Compact(result.Paths)
	return result
}

func dynamicPath(path string) bool { return strings.ContainsAny(path, "$`*?[]{}~") }

func simpleEffects(argv []string) CommandEffects {
	read := CommandEffects{Effect: ProvenRead}
	unknown := CommandEffects{Effect: UnknownEffects, Reason: "command effects are not proven; use a structured read or an enforced isolated execution profile"}
	if len(argv) == 0 {
		return read
	}
	// Environment assignments can load code (BASH_ENV/NODE_OPTIONS/LD_PRELOAD).
	// Even an ordinary-looking program is unproven under caller overrides.
	if strings.Contains(argv[0], "=") {
		return unknown
	}
	program := argv[0]
	if strings.Contains(program, "/") || dynamicPath(program) {
		return unknown
	}
	args := argv[1:]
	// A help flag is only a probe in the exact interpreter form, never a
	// property of a whole shell command or of a script argument.
	if slices.Contains([]string{"python", "python2", "python3", "node", "nodejs", "go", "npm", "npx", "yarn", "pnpm", "make", "cargo"}, program) {
		if len(args) == 1 && slices.Contains([]string{"--version", "--help", "-h", "-V"}, args[0]) {
			return read
		}
		return unknown
	}
	switch program {
	case "printf":
		// The shell builtin's %n and -v assign variables, including array
		// subscripts evaluated as shell arithmetic. Treat them as unproven.
		if len(args) == 0 || strings.HasPrefix(args[0], "-") || strings.Contains(args[0], "%n") {
			return unknown
		}
		return read
	case "date":
		// GNU date also sets the clock using a positional numeric operand.
		if len(args) == 0 || (len(args) == 1 && (strings.HasPrefix(args[0], "+") || slices.Contains([]string{"--help", "--version", "-u", "--utc", "-I", "--iso-8601", "-R"}, args[0]))) {
			return read
		}
		return unknown
	case "uniq":
		operands := 0
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				if !slices.Contains([]string{"-c", "-d", "-u", "-i", "--count", "--repeated", "--unique", "--ignore-case"}, arg) {
					return unknown
				}
			} else {
				operands++
			}
		}
		if operands > 1 {
			return unknown // second operand is an output path
		}
		return read
	case "echo", "cat", "head", "tail", "wc", "pwd", "ls", "stat", "file", "which", "true", "false", "uname", "sort", "cut", "tr", "od", "hexdump", "sha256sum", "shasum", "realpath", "basename", "dirname", "readlink":
		// Commands with explicit output options are handled conservatively.
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && ((program == "sort" && strings.Contains(arg, "o")) || (program == "file" && strings.Contains(arg, "C"))) {
				return unknown
			}
			if (program == "printf" && strings.HasPrefix(arg, "-v")) || (program == "sort" && strings.HasPrefix(arg, "--compress-program")) || (strings.HasPrefix(arg, "-o") && program == "sort") || arg == "-o" || (program == "file" && (arg == "-C" || arg == "--compile")) || strings.HasPrefix(arg, "--output") || strings.HasPrefix(arg, "--reference") || (program == "date" && (strings.HasPrefix(arg, "-s") || strings.HasPrefix(arg, "--set"))) {
				return unknown
			}
		}
		return read
	case "rg", "grep":
		for _, arg := range args {
			if strings.HasPrefix(arg, "--pre") {
				return unknown
			}
		}
		return read
	case "git":
		if len(args) == 0 || !slices.Contains([]string{"diff", "status", "log", "show", "rev-parse", "ls-files", "ls-tree", "diff-tree", "diff-index", "diff-files"}, args[0]) {
			return unknown
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "--output") || strings.HasPrefix(arg, "--ext-diff") || strings.HasPrefix(arg, "--textconv") || strings.HasPrefix(arg, "--open-files-in-pager") {
				return unknown
			}
		}
		return read
	case "tee", "touch", "mkdir", "rm":
		paths, ok := literalOperands(args, program)
		if !ok || len(paths) == 0 {
			return unknown
		}
		return CommandEffects{Effect: KnownWrites, Paths: paths}
	case "cp", "mv", "install":
		paths, ok := literalOperands(args, program)
		if !ok || len(paths) != 2 {
			return unknown
		}
		// mv removes the source as well. Both paths require authorization.
		if program == "mv" {
			return CommandEffects{Effect: KnownWrites, Paths: paths}
		}
		return CommandEffects{Effect: KnownWrites, Paths: paths[1:]}
	case "sed":
		// Resolve only the familiar literal in-place form. Other sed scripts
		// can execute programs or write files; their effects remain unknown.
		if len(args) == 3 && args[0] == "-i" && !dynamicPath(args[2]) && strings.HasPrefix(args[1], "s/") && !strings.ContainsAny(args[1], ";\n") {
			parts := strings.Split(args[1], "/")
			if len(parts) == 4 && !strings.ContainsAny(parts[3], "we") {
				return CommandEffects{Effect: KnownWrites, Paths: []string{args[2]}}
			}
		}
		return unknown
	default:
		return unknown
	}
}

func literalOperands(args []string, program string) ([]string, bool) {
	var paths []string
	flags := true
	for _, arg := range args {
		if arg == "--" && flags {
			flags = false
			continue
		}
		if flags && strings.HasPrefix(arg, "-") {
			allowed := map[string][]string{"tee": {"-a", "--append"}, "touch": {"-c", "--no-create"}, "mkdir": {"-p", "--parents"}, "rm": {"-f", "-r", "-rf", "-fr", "--force", "--recursive"}, "cp": {"-f", "-r", "-R", "-a", "--"}, "mv": {"-f", "-n"}}
			if !slices.Contains(allowed[program], arg) {
				return nil, false
			}
			continue
		}
		if arg == "" || dynamicPath(arg) || filepath.Clean(arg) == "." {
			return nil, false
		}
		paths = append(paths, arg)
	}
	return paths, true
}
