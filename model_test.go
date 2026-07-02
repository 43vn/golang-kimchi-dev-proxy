package main

import (
	"testing"
)

func TestResolveUpstreamModel_Passthrough(t *testing.T) {
	t.Setenv(anthropicModelEnv, "")

	got := ResolveUpstreamModel("claude-sonnet-4-6")
	if got != "claude-sonnet-4-6" {
		t.Fatalf("ResolveUpstreamModel() = %q, want passthrough", got)
	}
}

func TestResolveUpstreamModel_Override(t *testing.T) {
	t.Setenv(anthropicModelEnv, "minimax-m3")

	got := ResolveUpstreamModel("claude-sonnet-4-6")
	if got != "minimax-m3" {
		t.Fatalf("ResolveUpstreamModel() = %q, want minimax-m3", got)
	}
}

func TestResolveUpstreamModel_TrimsWhitespace(t *testing.T) {
	t.Setenv(anthropicModelEnv, "  kimi-k2.7  ")

	got := ResolveUpstreamModel("claude-opus-4-20250514")
	if got != "kimi-k2.7" {
		t.Fatalf("ResolveUpstreamModel() = %q, want trimmed override", got)
	}
}

func TestApplyModelToRequestBody(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":100}`)
	got := applyModelToRequestBody(body, "minimax-m3")
	want := `{"model":"minimax-m3","max_tokens":100}`
	if string(got) != want {
		t.Fatalf("applyModelToRequestBody() = %s, want %s", got, want)
	}
}