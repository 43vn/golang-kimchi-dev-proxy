package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	claude "kimchi/internal/translator/claude"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

// TestForwardStream_AnthropicSSEWireFormat ensures handler-style forwarding writes
// translator output verbatim (event:/data: lines), not double-wrapped.
func TestForwardStream_AnthropicSSEWireFormat(t *testing.T) {
	originalReq := []byte(`{"model":"claude-sonnet-4-6","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`)
	openAIChunk := []byte(`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"minimax-m3","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`)

	var param any
	chunks := claude.ConvertOpenAIResponseToClaude(context.Background(), "minimax-m3", originalReq, originalReq, openAIChunk, &param)
	chunks = append(chunks, claude.ConvertOpenAIResponseToClaude(
		context.Background(), "minimax-m3", originalReq, originalReq,
		[]byte(`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"minimax-m3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
		&param,
	)...)
	chunks = append(chunks, claude.ConvertOpenAIResponseToClaude(
		context.Background(), "minimax-m3", originalReq, originalReq,
		[]byte("data: [DONE]"),
		&param,
	)...)

	dataChan := make(chan StreamChunk, len(chunks))
	for _, chunk := range chunks {
		dataChan <- StreamChunk{Data: chunk}
	}
	close(dataChan)

	fr := &flushRecorder{httptest.NewRecorder()}
	chunkCount := 0
	ForwardStream(context.Background(), fr, fr, dataChan, StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			if len(chunk) == 0 {
				return
			}
			fr.Write(chunk)
			chunkCount++
		},
		WriteDone: func() {},
	})

	out := fr.Body.String()
	if strings.Contains(out, "data: event:") {
		t.Fatalf("SSE must not double-wrap event lines, got:\n%s", out)
	}
	if !strings.Contains(out, "event: message_start\n") {
		t.Fatalf("expected event: message_start, got:\n%s", out)
	}
	if !strings.Contains(out, "event: message_stop\n") {
		t.Fatalf("expected event: message_stop, got:\n%s", out)
	}
	if chunkCount == 0 {
		t.Fatal("expected at least one chunk written")
	}
}

// TestAnthropicStreaming_EndToEnd exercises the full handler path against a mock upstream.
func TestAnthropicStreaming_EndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"minimax-m3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"minimax-m3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	ginEngine := setupTestRouter()
	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)
	ginEngine.POST("/v1/messages", func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		var raw map[string]any
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		model, _ := raw["model"].(string)
		apiKey := extractAPIKey(c)
		handleStreamingWithExecutor(c, executor, apiKey, model, bodyBytes, "test-session", time.Now())
	})

	reqBody := map[string]any{
		"model":      "claude-sonnet-4-6",
		"max_tokens": 100,
		"stream":     true,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	ginEngine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if strings.Contains(out, "data: event:") {
		t.Fatalf("end-to-end SSE must not double-wrap, got:\n%s", out)
	}
	if !strings.Contains(out, "event: message_stop\n") {
		t.Fatalf("expected message_stop in end-to-end output, got:\n%s", out)
	}
}