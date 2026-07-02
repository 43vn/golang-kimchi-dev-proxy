package util

import "testing"

func TestUnescapeMiniMaxMarkup(t *testing.T) {
	escaped := `}]<]minimax[>[</old_string>]<]minimax[>[<old_string>func test() {]<]minimax[>[</old_string>]<]minimax[>[</invoke>]<]minimax[>[</tool_call>`
	want := `}</old_string><old_string>func test() {</old_string></invoke></tool_call>`

	if got := UnescapeMiniMaxMarkup(escaped); got != want {
		t.Fatalf("UnescapeMiniMaxMarkup() = %q, want %q", got, want)
	}
}

func TestSanitizeUpstreamText_OnlyMiniMax(t *testing.T) {
	escaped := `a]<]minimax[>[b`

	if got := SanitizeUpstreamText("gpt-4", "plain text"); got != "plain text" {
		t.Fatalf("plain text without escape token should be unchanged, got %q", got)
	}
	if got := SanitizeUpstreamText("gpt-4", escaped); got != "ab" {
		t.Fatalf("escape token should be removed even for non-minimax model alias, got %q", got)
	}
	if got := SanitizeUpstreamText("claude-sonnet-4-20250514", escaped); got != "ab" {
		t.Fatalf("claude model alias with minimax escapes should sanitize, got %q", got)
	}
	if got := SanitizeUpstreamText("minimax-m3", escaped); got != "ab" {
		t.Fatalf("minimax model should sanitize, got %q", got)
	}
}

func TestIsMiniMaxModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"minimax-m3", true},
		{"MiniMax-M2.7", true},
		{"kimi-k2.7", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsMiniMaxModel(tt.model); got != tt.want {
			t.Fatalf("IsMiniMaxModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}