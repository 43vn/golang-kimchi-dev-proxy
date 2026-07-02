package claude

import (
	"encoding/json"
	"testing"
)

func TestConvertClaudeRequestToOpenAI_Basic(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["model"] != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %v", parsed["model"])
	}
	if parsed["max_tokens"] != float64(100) {
		t.Errorf("expected max_tokens 100, got %v", parsed["max_tokens"])
	}
	if parsed["stream"] != false {
		t.Errorf("expected stream false, got %v", parsed["stream"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithTemperature(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"temperature":0.7,"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", parsed["temperature"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithTopP(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"top_p":0.9,"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["top_p"] != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", parsed["top_p"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithStopSequences(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"stop_sequences":["STOP","END"],"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	stop, ok := parsed["stop"].([]any)
	if !ok {
		t.Fatalf("expected stop to be array, got %T", parsed["stop"])
	}
	if len(stop) != 2 {
		t.Errorf("expected 2 stop sequences, got %d", len(stop))
	}
}

func TestConvertClaudeRequestToOpenAI_WithStream(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), true)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["stream"] != true {
		t.Errorf("expected stream true, got %v", parsed["stream"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithSystemString(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"system":"You are a helpful assistant","messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	messages, ok := parsed["messages"].([]any)
	if !ok {
		t.Fatalf("expected messages to be array, got %T", parsed["messages"])
	}

	// First message should be system
	if len(messages) == 0 {
		t.Fatal("expected at least 1 message")
	}
	sysMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first message to be object, got %T", messages[0])
	}
	if sysMsg["role"] != "system" {
		t.Errorf("expected first message role 'system', got %v", sysMsg["role"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithTools(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"tools":[{"name":"get_weather","description":"Get weather info","input_schema":{"type":"object","properties":{"location":{"type":"string"}}}}],"messages":[{"role":"user","content":"What's the weather?"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	tools, ok := parsed["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools to be array, got %T", parsed["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("expected tool to be object, got %T", tools[0])
	}
	if tool["type"] != "function" {
		t.Errorf("expected tool type 'function', got %v", tool["type"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithToolChoice(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["tool_choice"] != "auto" {
		t.Errorf("expected tool_choice 'auto', got %v", parsed["tool_choice"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithToolChoiceAny(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["tool_choice"] != "required" {
		t.Errorf("expected tool_choice 'required', got %v", parsed["tool_choice"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithToolChoiceTool(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"tool_choice":{"type":"tool","name":"get_weather"},"messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	toolChoice, ok := parsed["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_choice to be object, got %T", parsed["tool_choice"])
	}
	if toolChoice["type"] != "function" {
		t.Errorf("expected tool_choice type 'function', got %v", toolChoice["type"])
	}
}

func TestConvertClaudeRequestToOpenAI_WithUser(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"user":"test-user","messages":[{"role":"user","content":"Hello"}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if parsed["user"] != "test-user" {
		t.Errorf("expected user 'test-user', got %v", parsed["user"])
	}
}

func TestConvertClaudeRequestToOpenAI_ToolResult(t *testing.T) {
	input := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"get_weather","input":{"location":"NYC"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"Sunny, 75°F"}]}]}`
	result := ConvertClaudeRequestToOpenAI("gpt-4", []byte(input), false)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	messages, ok := parsed["messages"].([]any)
	if !ok {
		t.Fatalf("expected messages to be array, got %T", parsed["messages"])
	}

	// Should have assistant message with tool_calls and tool result message
	foundToolCall := false
	foundToolResult := false
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		if m["role"] == "assistant" {
			if _, ok := m["tool_calls"]; ok {
				foundToolCall = true
			}
		}
		if m["role"] == "tool" {
			foundToolResult = true
		}
	}

	if !foundToolCall {
		t.Error("expected assistant message with tool_calls")
	}
	if !foundToolResult {
		t.Error("expected tool result message")
	}
}
