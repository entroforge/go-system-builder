package classifier_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/classifier"
)

// TestTokenizeCommandSubstitutionInsideDoubleQuote covers BUG-007: a `$()`
// command substitution appearing inside a double-quoted string used to leak
// the outer `parens` counter and trip the final `parens != 0` check, which
// surfaced to users as a false-positive policy deny blocking legitimate
// curl-with-$(...) commands. The tokenizer must accept these inputs.
func TestTokenizeCommandSubstitutionInsideDoubleQuote(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{
			name: "simple sub in double quote",
			cmd:  `echo "x-token: $(curl -fsS http://localhost/api)"`,
		},
		{
			name: "sub with nested single-quoted JSON inside double quote",
			cmd:  `echo "x: $(curl -d '{"username":"admin","password":"123456"}')"`,
		},
		{
			name: "realistic curl | python3 inside double quote",
			cmd:  `echo "x-token: $(curl -fsS -X POST http://localhost:8080/api/base/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")"`,
		},
		{
			name: "assignment from unquoted sub with python3 inside",
			cmd:  `ADMIN_TOKEN=$(curl -fsS -X POST http://localhost:8080/api/base/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")`,
		},
		{
			name: "two adjacent subs in one double-quoted string",
			cmd:  `echo "$(date) $(whoami)"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := classifier.Tokenize(tc.cmd)
			if err != nil {
				t.Fatalf("Tokenize failed: %v\ncommand: %s", err, tc.cmd)
			}
			if len(tokens) == 0 {
				t.Fatalf("Tokenize produced no tokens for: %s", tc.cmd)
			}
			// Sanity: Resolve must also succeed so the policy engine does
			// not fall into its "Resolve error → block" branch.
			if _, err := classifier.Resolve(tc.cmd); err != nil {
				t.Fatalf("Resolve failed: %v\ncommand: %s", err, tc.cmd)
			}
		})
	}
}

// TestResolveCurlCommandSubstitutionNotFlaggedAsREQWrite is the end-to-end
// regression for the user-reported BUG-007 incident: realistic curl commands
// that embed `$()` for token acquisition must Resolve cleanly with no
// AffectedPaths, so `bound_req_write` does not block them.
func TestResolveCurlCommandSubstitutionNotFlaggedAsREQWrite(t *testing.T) {
	cmds := []string{
		`ADMIN_TOKEN=$(curl -fsS -X POST http://localhost:8080/api/base/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")`,
		`curl -fsS -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/funds/2/kyc-case-requirements -H "x-token: $(curl -fsS -X POST http://localhost:8080/api/base/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")"`,
	}
	for i, c := range cmds {
		t.Run(strings.Fields(c)[0]+"/"+strconv.Itoa(i), func(t *testing.T) {
			resolved, err := classifier.Resolve(c)
			if err != nil {
				t.Fatalf("Resolve: %v\ncommand: %s", err, c)
			}
			if len(resolved.AffectedPaths) > 0 {
				t.Fatalf("expected no AffectedPaths for HTTP-only command, got: %v\ncommand: %s",
					resolved.AffectedPaths, c)
			}
		})
	}
}
