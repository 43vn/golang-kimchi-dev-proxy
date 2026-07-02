package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	claude "kimchi/internal/translator/claude"
)

// StreamChunk represents a chunk of streaming data
type StreamChunk struct {
	Data  []byte
	Error error
}

// StreamExecutor handles streaming requests to upstream
type StreamExecutor struct {
	client      *http.Client
	upstreamURL string
}

// NewStreamExecutor creates a new stream executor
func NewStreamExecutor(client *http.Client, upstreamURL string) *StreamExecutor {
	return &StreamExecutor{
		client:      client,
		upstreamURL: upstreamURL,
	}
}

// ExecuteStream executes a streaming request and returns channels for data and errors
func (e *StreamExecutor) ExecuteStream(
	ctx context.Context,
	apiKey string,
	modelName string,
	originalRequest []byte,
	stream bool,
) (<-chan StreamChunk, http.Header) {
	dataChan := make(chan StreamChunk, 100)

	// Convert Anthropic request to OpenAI format
	convertedBody := claude.ConvertClaudeRequestToOpenAI(modelName, originalRequest, stream)

	// Create upstream request
	outReq, err := http.NewRequestWithContext(ctx, "POST", e.upstreamURL, bytes.NewReader(convertedBody))
	if err != nil {
		close(dataChan)
		return dataChan, nil
	}

	outReq.Header.Set("Content-Type", "application/json")
	outReq.Header.Set("Accept", "application/json")
	outReq.Header.Set("Authorization", "Bearer "+apiKey)
	outReq.Header.Set("User-Agent", "kimchi/0.1.52")
	outReq.Header.Set("X-Session-Id", uuid.NewString())

	// Execute request
	resp, err := e.client.Do(outReq)
	if err != nil {
		dataChan <- StreamChunk{Error: err}
		close(dataChan)
		return dataChan, nil
	}

	// Clone headers for downstream
	upstreamHeaders := cloneHeader(resp.Header)

	if stream {
		e.handleStreamingResponse(ctx, resp, dataChan, modelName, originalRequest)
	} else {
		e.handleNonStreamingResponse(ctx, resp, dataChan, modelName, originalRequest)
	}

	return dataChan, upstreamHeaders
}

// handleStreamingResponse handles streaming response from upstream
func (e *StreamExecutor) handleStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	dataChan chan<- StreamChunk,
	modelName string,
	originalRequest []byte,
) {
	go func() {
		defer close(dataChan)
		defer resp.Body.Close()

		// If upstream returned error, send error chunk
		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp.Body)
			dataChan <- StreamChunk{
				Error: &UpstreamError{
					StatusCode: resp.StatusCode,
					Body:       respBody,
				},
			}
			return
		}

		// Stream response body
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// Streaming state for response conversion
		var param any

		for scanner.Scan() {
			line := scanner.Text()

			// Pass through empty lines and SSE comments/control fields.
			// CLIProxyAPI-style: skip `event:`, `id:`, `retry:` SSE fields as
			// well as the standard `:` comment line — these carry no payload.
			if line == "" ||
				strings.HasPrefix(line, ":") ||
				strings.HasPrefix(line, "event:") ||
				strings.HasPrefix(line, "id:") ||
				strings.HasPrefix(line, "retry:") {
				continue
			}

			// Only process data lines
			if !strings.HasPrefix(line, "data: ") {
				// Upstream error detection (kimchi extension over CLIProxyAPI):
				// If upstream returns a non-SSE body (raw JSON, array, or
				// XML/HTML) inside a 2xx response, treat it as a bad-gateway
				// error instead of silently dropping it.
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "{") ||
					strings.HasPrefix(trimmed, "[") ||
					strings.HasPrefix(trimmed, "<") {
					log.Printf("[STREAM_ERROR] upstream returned non-SSE body (truncated): %s", truncateBody([]byte(line), 500))
					dataChan <- StreamChunk{
						Error: &UpstreamError{
							StatusCode: http.StatusBadGateway,
							Body:       []byte(line),
						},
					}
					return
				}
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			// Convert OpenAI streaming chunk to Anthropic format
			converted := claude.ConvertOpenAIResponseToClaude(
				ctx,
				modelName,
				originalRequest,
				originalRequest,
				[]byte("data: "+data),
				&param,
			)

			// Send each converted event
			for _, chunk := range converted {
				if len(chunk) > 0 {
					dataChan <- StreamChunk{Data: chunk}
				}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			log.Printf("[STREAM_ERROR] scanner error: %v", errScan)
			dataChan <- StreamChunk{Error: errScan}
		} else {
			// CLIProxyAPI-style synthetic [DONE] handling:
			// In case the upstream closed the stream without a terminal [DONE]
			// marker, feed a synthetic done marker through the converter so
			// pending events (e.g. message_stop) are still emitted exactly once.
			doneConverted := claude.ConvertOpenAIResponseToClaude(
				ctx,
				modelName,
				originalRequest,
				originalRequest,
				[]byte("data: [DONE]"),
				&param,
			)
			for _, chunk := range doneConverted {
				if len(chunk) > 0 {
					dataChan <- StreamChunk{Data: chunk}
				}
			}
		}
	}()
}

// handleNonStreamingResponse handles non-streaming response from upstream
func (e *StreamExecutor) handleNonStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	dataChan chan<- StreamChunk,
	modelName string,
	originalRequest []byte,
) {
	go func() {
		defer close(dataChan)
		defer resp.Body.Close()

		// Read entire response body
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			dataChan <- StreamChunk{Error: err}
			return
		}

		// If upstream returned error, send error chunk
		if resp.StatusCode >= 400 {
			dataChan <- StreamChunk{
				Error: &UpstreamError{
					StatusCode: resp.StatusCode,
					Body:       respBody,
				},
			}
			return
		}

		// Validate response body is JSON before parsing.
		// Upstream may return 2xx with non-JSON body (XML/HTML/text) which would
		// otherwise produce a silently empty Anthropic response.
		trimmed := bytes.TrimSpace(respBody)
		if !json.Valid(trimmed) {
			log.Printf("[UPSTREAM_ERROR] non-JSON response body (truncated): %s", truncateBody(respBody, 500))
			dataChan <- StreamChunk{
				Error: &UpstreamError{
					StatusCode: http.StatusBadGateway,
					Body:       respBody,
				},
			}
			return
		}

		// Convert OpenAI response to Anthropic format
		var param any
		converted := claude.ConvertOpenAIResponseToClaudeNonStream(
			ctx,
			modelName,
			originalRequest,
			originalRequest,
			respBody,
			&param,
		)

		dataChan <- StreamChunk{Data: converted}
	}()
}

// UpstreamError represents an error from upstream
type UpstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *UpstreamError) Error() string {
	return string(e.Body)
}

// cloneHeader clones http.Header
func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	newH := make(http.Header, len(h))
	for k, vv := range h {
		newVv := make([]string, len(vv))
		copy(newVv, vv)
		newH[k] = newVv
	}
	return newH
}

// StreamForwardOptions configures stream forwarding
type StreamForwardOptions struct {
	// WriteChunk writes a single data chunk to the response body
	WriteChunk func(chunk []byte)

	// WriteTerminalError writes an error payload when streaming fails
	WriteTerminalError func(errMsg error)

	// WriteDone writes a terminal marker when upstream data channel closes
	WriteDone func()

	// WriteKeepAlive writes a keep-alive heartbeat
	WriteKeepAlive func()
}

// ForwardStream forwards data from channel to HTTP response
func ForwardStream(
	ctx context.Context,
	w io.Writer,
	flusher http.Flusher,
	dataChan <-chan StreamChunk,
	opts StreamForwardOptions,
) {
	writeChunk := opts.WriteChunk
	if writeChunk == nil {
		writeChunk = func(chunk []byte) {
			w.Write([]byte("data: "))
			w.Write(chunk)
			w.Write([]byte("\n\n"))
		}
	}

	writeKeepAlive := opts.WriteKeepAlive
	if writeKeepAlive == nil {
		writeKeepAlive = func() {
			w.Write([]byte(": keep-alive\n\n"))
		}
	}

	writeDone := opts.WriteDone
	if writeDone == nil {
		writeDone = func() {
			w.Write([]byte("data: [DONE]\n\n"))
		}
	}

	// Keep-alive ticker
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	var terminalErr error
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-dataChan:
			if !ok {
				// Channel closed
				if terminalErr != nil {
					if opts.WriteTerminalError != nil {
						opts.WriteTerminalError(terminalErr)
					}
					flusher.Flush()
					return
				}
				writeDone()
				flusher.Flush()
				return
			}

			if chunk.Error != nil {
				terminalErr = chunk.Error
				log.Printf("[STREAM_ERROR] err=%v", chunk.Error)
				continue
			}

			writeChunk(chunk.Data)
			flusher.Flush()

		case <-keepAlive.C:
			writeKeepAlive()
			flusher.Flush()
		}
	}
}
