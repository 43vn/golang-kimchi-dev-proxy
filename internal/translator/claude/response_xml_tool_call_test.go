package claude

import (
	"context"
	"strings"
	"testing"
)

func TestConvertOpenAIResponseToClaude_XMLToolCallInContent(t *testing.T) {
	prose := "Em thay phần parser cũng sang map[string]any:"
	xmlPart := `<tool_call>
<invoke name="Edit"><replace_all>false</replace_all><file_path>/home/vincent/dev/golang/tao-anh-server/backend/internal/oauth/antigravity/auth.go</file_path><old_string>struct</old_string><new_string>map[string]any</new_string></invoke>
</tool_call>`
	content := prose + xmlPart

	chunk := `data: {"id":"chatcmpl-xml","object":"chat.completion.chunk","created":1,"model":"claude-sonnet-4-20250514","choices":[{"index":0,"delta":{"content":` + quoteJSON(content) + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`
	originalReq := `{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Edit"}]}`

	var param any
	chunks := ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(originalReq),
		[]byte(originalReq),
		[]byte(chunk),
		&param,
	)
	chunks = append(chunks, ConvertOpenAIResponseToClaude(
		context.Background(),
		"claude-sonnet-4-20250514",
		[]byte(originalReq),
		[]byte(originalReq),
		[]byte("data: [DONE]"),
		&param,
	)...)

	joined := string(concatChunks(chunks))

	if strings.Contains(joined, "<tool_call>") || strings.Contains(joined, "<invoke") {
		t.Fatalf("expected no raw XML in SSE output, got: %s", joined)
	}
	if !strings.Contains(joined, "text_delta") || !strings.Contains(joined, prose) {
		t.Fatalf("expected prose in text_delta, got: %s", joined)
	}
	if !strings.Contains(joined, "tool_use") {
		t.Fatal("expected tool_use content block")
	}
	if !strings.Contains(joined, "input_json_delta") {
		t.Fatal("expected input_json_delta for parsed XML tool call")
	}
	if !strings.Contains(joined, `"name":"Edit"`) && !strings.Contains(joined, `Edit`) {
		t.Fatalf("expected Edit tool name in output, got: %s", joined)
	}
	if !strings.Contains(joined, "tool_use") || !strings.Contains(joined, "stop_reason") {
		t.Fatalf("expected tool_use stop_reason, got: %s", joined)
	}
}

func quoteJSON(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '\\':
			b = append(b, `\`...)
		case '"':
			b = append(b, `\"`...)
		case '\n':
			b = append(b, `\n`...)
		case '\r':
			b = append(b, `\r`...)
		case '\t':
			b = append(b, `\t`...)
		default:
			b = append(b, string(r)...)
		}
	}
	b = append(b, '"')
	return string(b)
}

func concatChunks(chunks [][]byte) []byte {
	var out []byte
	for _, c := range chunks {
		out = append(out, c...)
		out = append(out, '\n')
	}
	return out
}