package claude

import (
	"context"
	"strings"
	"testing"
)

func TestConvertOpenAIResponseToClaude_MiniMaxToolArgumentUnescape(t *testing.T) {
	runCase := func(t *testing.T, responseModel, requestModel string) {
		t.Helper()
		escapedArgs := `{"old_string":"}]<]minimax[>[</old_string>]<]minimax[>[<old_string>func test() {]<]minimax[>[</old_string>"}`
		input := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"` + responseModel + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"StrReplace","arguments":` + escapedArgs + `}}]},"finish_reason":"tool_calls"}]}`
		originalReq := `{"model":"` + requestModel + `","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"StrReplace"}]}`

		var param any
		chunks := ConvertOpenAIResponseToClaude(
			context.Background(),
			requestModel,
			[]byte(originalReq),
			[]byte(originalReq),
			[]byte(input),
			&param,
		)
		chunks = append(chunks, ConvertOpenAIResponseToClaude(
			context.Background(),
			requestModel,
			[]byte(originalReq),
			[]byte(originalReq),
			[]byte("data: [DONE]"),
			&param,
		)...)

		var partial string
		for _, chunk := range chunks {
			str := string(chunk)
			if strings.Contains(str, "input_json_delta") {
				partial = str
			}
		}
		if partial == "" {
			t.Fatal("expected input_json_delta event")
		}
		if strings.Contains(partial, "]<]minimax[>[") {
			t.Fatalf("expected escaped minimax markup removed, got: %s", partial)
		}
		if !strings.Contains(partial, `\u003c/old_string\u003e`) {
			t.Fatalf("expected restored XML tags in partial_json, got: %s", partial)
		}
	}

	t.Run("minimax model name", func(t *testing.T) {
		runCase(t, "minimax-m3", "minimax-m3")
	})
	t.Run("claude alias from Claude Code", func(t *testing.T) {
		runCase(t, "claude-sonnet-4-20250514", "claude-sonnet-4-20250514")
	})
}