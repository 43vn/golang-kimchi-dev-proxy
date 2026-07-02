package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// drainChunks reads all chunks from the data channel until it closes or timeout
func drainChunks(t *testing.T, ch <-chan StreamChunk, timeout time.Duration) []StreamChunk {
	t.Helper()
	var chunks []StreamChunk
	deadline := time.After(timeout)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return chunks
			}
			chunks = append(chunks, c)
		case <-deadline:
			t.Fatalf("timed out waiting for chunks after %v", timeout)
		}
	}
}

// TestExecuteStream_NonSSEBody_HTMLUpstreamError verifies that streaming
// requests detect HTML/XML/JSON error bodies from upstream and return them as
// UpstreamError chunks instead of silently producing empty streams.
func TestExecuteStream_NonSSEBody_HTMLUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "<html><body><h1>502 Bad Gateway</h1></body></html>\n")
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk for non-SSE upstream body")
	}

	var upstreamErr *UpstreamError
	var found bool
	for _, c := range chunks {
		if c.Error != nil {
			if ue, ok := c.Error.(*UpstreamError); ok {
				upstreamErr = ue
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected UpstreamError chunk, got chunks=%+v", chunks)
	}
	if upstreamErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", upstreamErr.StatusCode)
	}
	if string(upstreamErr.Body) == "" {
		t.Error("expected error body to be populated")
	}
}

// TestExecuteStream_NonSSEBody_RAWJSON verifies detection of raw JSON error
// inside what should be an SSE stream.
func TestExecuteStream_NonSSEBody_RAWJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"error":{"message":"something went wrong"}}`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatalf("expected UpstreamError for raw JSON body, got chunks=%+v", chunks)
	}
	if upstreamErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", upstreamErr.StatusCode)
	}
}

// TestExecuteStream_ValidSSE verifies normal SSE stream still works.
func TestExecuteStream_ValidSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, `data: [DONE]`+"\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk for valid SSE")
	}

	// Should produce data chunks, no error
	for _, c := range chunks {
		if c.Error != nil {
			t.Errorf("unexpected error chunk: %v", c.Error)
		}
		if len(c.Data) == 0 {
			t.Error("unexpected empty data chunk")
		}
	}
}

// TestExecuteStream_SSEControlFields verifies that SSE control lines
// (event:, id:, retry:, comments) are skipped without triggering upstream
// error detection — matching CLIProxyAPI behavior.
func TestExecuteStream_SSEControlFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Mix control fields with data lines
		_, _ = io.WriteString(w, ": keep-alive comment\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "id: 1\n")
		_, _ = io.WriteString(w, "retry: 5000\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, `data: [DONE]`+"\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// No errors expected — control fields must be silently skipped
	for _, c := range chunks {
		if c.Error != nil {
			t.Errorf("SSE control fields should be skipped, got error: %v", c.Error)
		}
	}
}

// TestExecuteStream_StreamingXMLBody verifies XML body detection during
// streaming works (kimchi extension over CLIProxyAPI).
func TestExecuteStream_StreamingXMLBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><error>oops</error>`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected upstream error chunk for XML body")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatalf("expected UpstreamError for streaming XML body, got chunks=%+v", chunks)
	}
	if upstreamErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", upstreamErr.StatusCode)
	}
}

// TestExecuteStream_NonStreaming_XMLResponse verifies non-streaming path
// detects XML/HTML upstream body and returns UpstreamError.
func TestExecuteStream_NonStreaming_XMLResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><error><message>upstream failure</message></error>`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, false)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk for XML upstream body")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatalf("expected UpstreamError for XML body, got chunks=%+v", chunks)
	}
	if upstreamErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", upstreamErr.StatusCode)
	}
}

// TestExecuteStream_NonStreaming_PlainTextResponse verifies plain text response
// (not JSON, not XML) is also detected.
func TestExecuteStream_NonStreaming_PlainTextResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `internal error: database unavailable`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, false)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatalf("expected UpstreamError for plain text body, got chunks=%+v", chunks)
	}
}

// TestExecuteStream_NonStreaming_ValidJSON verifies normal non-streaming JSON
// response still passes through correctly.
func TestExecuteStream_NonStreaming_ValidJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"Hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, false)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	for _, c := range chunks {
		if c.Error != nil {
			t.Errorf("unexpected error for valid JSON: %v", c.Error)
		}
		if len(c.Data) == 0 {
			t.Error("unexpected empty data chunk for valid JSON response")
		}
	}
}

// TestUpstreamError_Error verifies UpstreamError.Error() returns the body.
func TestUpstreamError_Error(t *testing.T) {
	body := []byte(`{"error":"bad request"}`)
	err := &UpstreamError{StatusCode: 400, Body: body}
	if err.Error() != string(body) {
		t.Errorf("expected error message %q, got %q", string(body), err.Error())
	}
}

// TestExecuteStream_NonStreaming_4xxJSON verifies upstream 4xx with JSON body
// is still treated as upstream error (not silently passing through).
func TestExecuteStream_NonStreaming_4xxJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, false)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected error chunk")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatal("expected UpstreamError for 401 response")
	}
	if upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", upstreamErr.StatusCode)
	}
}

// TestExecuteStream_Streaming_4xxJSON verifies upstream 4xx with JSON body
// during streaming is treated as upstream error.
func TestExecuteStream_Streaming_4xxJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	executor := NewStreamExecutor(client, upstream.URL)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, true)

	chunks := drainChunks(t, dataChan, 5*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected error chunk")
	}

	var upstreamErr *UpstreamError
	for _, c := range chunks {
		if ue, ok := c.Error.(*UpstreamError); ok {
			upstreamErr = ue
			break
		}
	}
	if upstreamErr == nil {
		t.Fatal("expected UpstreamError for 400 response")
	}
	if upstreamErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", upstreamErr.StatusCode)
	}
}

// Sanity check: ensure generic errors are also surfaced (e.g. connection failure).
func TestExecuteStream_ConnectionError(t *testing.T) {
	// Use an unbindable URL (or a closed server) to force a connection error.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := upstream.URL
	upstream.Close() // immediately close to ensure connection error

	client := &http.Client{Timeout: 1 * time.Second}
	executor := NewStreamExecutor(client, url)

	originalReq := []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	dataChan, _ := executor.ExecuteStream(context.Background(), "test-key", "claude-sonnet-4-20250514", originalReq, false)

	chunks := drainChunks(t, dataChan, 3*time.Second)
	if len(chunks) == 0 {
		t.Fatal("expected error chunk for connection failure")
	}
	var foundErr error
	for _, c := range chunks {
		if c.Error != nil {
			foundErr = c.Error
			break
		}
	}
	if foundErr == nil {
		t.Fatalf("expected connection error, got chunks=%+v", chunks)
	}
	if foundErr.Error() == "" {
		t.Error("expected non-empty error message")
	}
}
