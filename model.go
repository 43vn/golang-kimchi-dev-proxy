package main

import (
	"os"
	"strings"

	"github.com/tidwall/sjson"
)

const anthropicModelEnv = "ANTHROPIC_MODEL"

// ResolveUpstreamModel returns the model name to send upstream for Anthropic /v1/messages.
// When ANTHROPIC_MODEL is set, client model aliases (e.g. claude-sonnet-4-6) are mapped
// to that value. OpenAI /v1/chat/completions requests are not affected.
func ResolveUpstreamModel(requested string) string {
	if override := strings.TrimSpace(os.Getenv(anthropicModelEnv)); override != "" {
		return override
	}
	return requested
}

// applyModelToRequestBody rewrites the top-level "model" field in a JSON request body.
func applyModelToRequestBody(body []byte, model string) []byte {
	out, err := sjson.SetBytes(body, "model", model)
	if err != nil {
		return body
	}
	return out
}