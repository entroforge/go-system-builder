package classifier_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/classifier"
)

func TestClassifyTestCommand(t *testing.T) {
	result := classifier.ClassifyCommand("go test ./...")
	if result.Class != "test" || result.Unknown {
		t.Fatalf("unexpected classification: %#v", result)
	}
}

func TestClassifyDependencyMutation(t *testing.T) {
	result := classifier.ClassifyCommand("npm install lodash")
	if result.Class != "dependency_mutation" || result.Unknown {
		t.Fatalf("unexpected classification: %#v", result)
	}
}

func TestClassifyOrdinaryGitMergeAsGit(t *testing.T) {
	result := classifier.ClassifyCommand("git merge main")
	if result.Class != "git" || result.Unknown {
		t.Fatalf("ordinary merge must remain an allowed git command: %#v", result)
	}
}

func TestParseSquashMergeUsesTokenizedShapes(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{command: "git merge feature --squash", want: "git merge --squash"},
		{command: "git -C /tmp/repo merge --squash feature", want: "git merge --squash"},
		{command: "gh pr merge 39 --squash", want: "gh pr merge --squash"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			got, matched := classifier.ParseSquashMerge(tc.command)
			if !matched || got != tc.want {
				t.Fatalf("ParseSquashMerge(%q) = %q, %t; want %q, true", tc.command, got, matched, tc.want)
			}
		})
	}
}

func TestParseSquashMergeAllowsNonSquashAndUnknownCommands(t *testing.T) {
	commands := []string{
		"git merge feature",
		"gh pr merge 39 --merge",
		`echo "git merge --squash feature"`,
		"custom-tool --squash",
		"git merge 'unterminated",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			if got, matched := classifier.ParseSquashMerge(command); matched {
				t.Fatalf("ParseSquashMerge(%q) = %q, true; want unmatched", command, got)
			}
		})
	}
}

func TestClassifyUnknownShellComposition(t *testing.T) {
	result := classifier.ClassifyCommand("go test ./... > /tmp/test.log")
	if !result.Unknown {
		t.Fatalf("redirection must require conservative handling: %#v", result)
	}
}
