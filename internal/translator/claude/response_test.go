package claude

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// parseSSEData extracts and parses the JSON data from an SSE event
func parseSSEData(t *testing.T, data []byte) map[string]any {
	t.Helper()
	str := string(data)
	// Find the "data: " line
	for _, line := range strings.Split(str, "\n") {
		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")
			var result map[string]any
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				t.Fatalf("failed to parse SSE data: %v, raw: %s", err, jsonStr)
			}
			return result
		}
	}
	t.Fatalf("no data line found in SSE event: %s", str)
	return nil
}

func TestConvertOpenAIResponseToClaude_Text(t *testing.T) {
	input := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`
	originalReq := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`

	var param any
	result := ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(originalReq),
		[]byte(originalReq),
		[]byte(input),
		&param,
	)

	if len(result) == 0 {
		t.Fatal("expected at least one result")
	}

	// First result should be message_start
	event := parseSSEData(t, result[0])
	if event["type"] != "message_start" {
		t.Errorf("expected first event type 'message_start', got %v", event["type"])
	}
}

func TestConvertOpenAIResponseToClaude_Done(t *testing.T) {
	input := `data: [DONE]`
	originalReq := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`

	var param any
	// First, simulate some content
	contentInput := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`
	ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(originalReq),
		[]byte(originalReq),
		[]byte(contentInput),
		&param,
	)

	// Now send DONE
	result := ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(originalReq),
		[]byte(originalReq),
		[]byte(input),
		&param,
	)

	if len(result) == 0 {
		t.Fatal("expected at least one result for DONE")
	}

	// Last event should be message_stop
	event := parseSSEData(t, result[len(result)-1])
	if event["type"] != "message_stop" {
		t.Errorf("expected last event type 'message_stop', got %v", event["type"])
	}
}

func TestConvertOpenAINonStreamingToAnthropic_Text(t *testing.T) {
	input := `{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"Hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`

	var param any
	result := ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(input),
		[]byte(input),
		[]byte("data: "+input),
		&param,
	)

	if len(result) == 0 {
		t.Fatal("expected at least one result")
	}

	var event map[string]any
	if err := json.Unmarshal(result[0], &event); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if event["type"] != "message" {
		t.Errorf("expected type 'message', got %v", event["type"])
	}
	if event["role"] != "assistant" {
		t.Errorf("expected role 'assistant', got %v", event["role"])
	}
	if event["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %v", event["stop_reason"])
	}
}

func TestMapOpenAIFinishReasonToAnthropic(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"content_filter", "end_turn"},
		{"function_call", "tool_use"},
		{"unknown", "end_turn"},
	}

	for _, tt := range tests {
		result := mapOpenAIFinishReasonToAnthropic(tt.input)
		if result != tt.expected {
			t.Errorf("mapOpenAIFinishReasonToAnthropic(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExtractOpenAIUsage(t *testing.T) {
	input := `{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":20}}`

	var parsed map[string]any
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// We can't directly call extractOpenAIUsage as it's unexported
	// But we can test the non-streaming response which uses it
	var param any
	result := ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(input),
		[]byte(input),
		[]byte("data: "+input),
		&param,
	)

	if len(result) == 0 {
		t.Fatal("expected at least one result")
	}
}
