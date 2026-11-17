package agent

import "testing"

func TestShouldPromptRedirection(t *testing.T) {
	cases := map[string]bool{
		// genuinely read-only: no approval prompt
		"ls":                   false,
		"git status":           false,
		"cat README.md":        false,
		"ls | wc -l":           false,
		"git status | head -5": false,
		"FOO=bar cat x":        false,
		// state-changing or unclassified: prompt
		"echo hi > file":            true,
		"echo x >> ~/.bashrc":       true,
		"cat a > b":                 true,
		"ls > out.txt":              true,
		"ls >out.txt":               true,
		"ls; echo x > f":            true,
		"git status && touch a":     true,
		"git push":                  true,
		"rm file":                   true,
	}
	for cmd, want := range cases {
		if got := ShouldPrompt(cmd); got != want {
			t.Errorf("ShouldPrompt(%q) = %v, want %v", cmd, got, want)
		}
	}
}
