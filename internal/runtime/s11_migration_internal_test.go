package runtime

import "testing"

func TestRolloverTerminalStateContract(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{state: "release_authorized", want: true},
		{state: "aborted", want: true},
		{state: "awaiting_human_release", want: false},
		{state: "", want: false},
		{state: "release", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := isRolloverTerminalState(tt.state); got != tt.want {
				t.Fatalf("isRolloverTerminalState(%q) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}
