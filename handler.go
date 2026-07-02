package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// logError logs error details for debugging
func logError(prefix string, statusCode int, body []byte, err error) {
	if err != nil {
		log.Printf("[ERROR] %s: status=%d err=%v", prefix, statusCode, err)
		return
	}
	// Truncate large error bodies for logging
	bodyStr := string(body)
	if len(bodyStr) > 2000 {
		bodyStr = bodyStr[:2000] + "... (truncated)"
	}
	log.Printf("[ERROR] %s: status=%d body=%s", prefix, statusCode, bodyStr)
}

// logUpstreamError logs upstream error responses with request context
func logUpstreamError(method, url string, statusCode int, respBody []byte, sessionID string) {
	bodyStr := string(respBody)
	if len(bodyStr) > 2000 {
		bodyStr = bodyStr[:2000] + "... (truncated)"
	}
	log.Printf("[UPSTREAM_ERROR] session=%s method=%s url=%s status=%d body=%s",
		sessionID, method, url, statusCode, bodyStr)
}

// AnthropicErrorResponse represents an Anthropic error response
type AnthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicErrorJSON creates an Anthropic error response JSON
func anthropicErrorJSON(errorType, message string) []byte {
	resp := AnthropicErrorResponse{}
	resp.Error.Type = errorType
	resp.Error.Message = message
	data, _ := json.Marshal(resp)
	return data
}

// extractAPIKey extracts API key from either x-api-key or Authorization header
func extractAPIKey(c *gin.Context) string {
	if key := c.GetHeader("x-api-key"); key != "" {
		return key
	}
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// anthropicMessagesHandler handles POST /v1/messages
func anthropicMessagesHandler(client *http.Client) gin.HandlerFunc {
	executor := NewStreamExecutor(client, upstreamURL)

	return func(c *gin.Context) {
		startTime := time.Now()
		sessionID := uuid.NewString()

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Printf("[REQUEST] session=%s error=read_body err=%v", sessionID, err)
			c.Data(http.StatusBadRequest, "application/json", anthropicErrorJSON("invalid_request_error", "failed to read body"))
			return
		}
		c.Request.Body.Close()

		// Validate request has required fields
		var raw map[string]any
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			log.Printf("[REQUEST] session=%s error=invalid_json err=%v body=%s", sessionID, err, truncateBody(bodyBytes, 500))
			c.Data(http.StatusBadRequest, "application/json", anthropicErrorJSON("invalid_request_error", "invalid JSON: "+err.Error()))
			return
		}

		model, _ := raw["model"].(string)
		if model == "" {
			log.Printf("[REQUEST] session=%s error=missing_model body=%s", sessionID, truncateBody(bodyBytes, 500))
			c.Data(http.StatusBadRequest, "application/json", anthropicErrorJSON("invalid_request_error", "model is required"))
			return
		}

		// Extract API key
		apiKey := extractAPIKey(c)
		if apiKey == "" {
			log.Printf("[REQUEST] session=%s error=missing_api_key model=%s", sessionID, model)
			c.Data(http.StatusUnauthorized, "application/json", anthropicErrorJSON("authentication_error", "missing API key"))
			return
		}

		// Check if streaming
		stream, _ := raw["stream"].(bool)

		log.Printf("[REQUEST] session=%s model=%s stream=%v body_size=%d", sessionID, model, stream, len(bodyBytes))

		if stream {
			handleStreamingWithExecutor(c, executor, apiKey, model, bodyBytes, sessionID, startTime)
		} else {
			handleNonStreamingWithExecutor(c, executor, apiKey, model, bodyBytes, sessionID, startTime)
		}
	}
}

// handleStreamingWithExecutor handles streaming using channel-based architecture
func handleStreamingWithExecutor(c *gin.Context, executor *StreamExecutor, apiKey, model string, bodyBytes []byte, sessionID string, startTime time.Time) {
	ctx := c.Request.Context()
	dataChan, upstreamHeaders := executor.ExecuteStream(ctx, apiKey, model, bodyBytes, true)

	// Set streaming headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	// Copy upstream headers
	if upstreamHeaders != nil {
		for k, vv := range upstreamHeaders {
			for _, v := range vv {
				c.Writer.Header().Set(k, v)
			}
		}
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	chunkCount := 0
	var terminalErr error

	ForwardStream(ctx, c.Writer, flusher, dataChan, StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			c.Writer.Write([]byte("data: "))
			c.Writer.Write(chunk)
			c.Writer.Write([]byte("\n\n"))
			chunkCount++
		},
		WriteTerminalError: func(err error) {
			terminalErr = err
			log.Printf("[STREAM_ERROR] session=%s err=%v", sessionID, err)

			// Write error as SSE event
			if upstreamErr, ok := err.(*UpstreamError); ok {
				c.Writer.Write([]byte("data: "))
				c.Writer.Write(upstreamErr.Body)
				c.Writer.Write([]byte("\n\n"))
			}
		},
		WriteDone: func() {
			log.Printf("[STREAM_DONE] session=%s chunks=%d duration=%v", sessionID, chunkCount, time.Since(startTime))
		},
		WriteKeepAlive: func() {
			c.Writer.Write([]byte(": keep-alive\n\n"))
		},
	})

	if terminalErr != nil {
		log.Printf("[STREAM_TERMINAL] session=%s err=%v chunks=%d duration=%v",
			sessionID, terminalErr, chunkCount, time.Since(startTime))
	}
}

// handleNonStreamingWithExecutor handles non-streaming using executor
func handleNonStreamingWithExecutor(c *gin.Context, executor *StreamExecutor, apiKey, model string, bodyBytes []byte, sessionID string, startTime time.Time) {
	ctx := c.Request.Context()
	dataChan, _ := executor.ExecuteStream(ctx, apiKey, model, bodyBytes, false)

	// Collect all chunks
	var result []byte
	for chunk := range dataChan {
		if chunk.Error != nil {
			log.Printf("[RESPONSE_ERROR] session=%s err=%v", sessionID, chunk.Error)
			if upstreamErr, ok := chunk.Error.(*UpstreamError); ok {
				c.Data(upstreamErr.StatusCode, "application/json", upstreamErr.Body)
			} else {
				c.Data(http.StatusBadGateway, "application/json", anthropicErrorJSON("api_error", chunk.Error.Error()))
			}
			return
		}
		result = append(result, chunk.Data...)
	}

	log.Printf("[RESPONSE] session=%s body_size=%d duration=%v", sessionID, len(result), time.Since(startTime))
	c.Data(http.StatusOK, "application/json", result)
}

// anthropicCountTokensHandler handles POST /v1/messages/count_tokens
func anthropicCountTokensHandler(client *http.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := uuid.NewString()

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Printf("[COUNT_TOKENS] session=%s error=read_body err=%v", sessionID, err)
			c.Data(http.StatusBadRequest, "application/json", anthropicErrorJSON("invalid_request_error", "failed to read body"))
			return
		}
		c.Request.Body.Close()

		apiKey := extractAPIKey(c)
		if apiKey == "" {
			log.Printf("[COUNT_TOKENS] session=%s error=missing_api_key", sessionID)
			c.Data(http.StatusUnauthorized, "application/json", anthropicErrorJSON("authentication_error", "missing API key"))
			return
		}

		// Forward to upstream count_tokens endpoint (if supported)
		// For now, return a simple token count estimate
		var raw map[string]any
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			log.Printf("[COUNT_TOKENS] session=%s error=invalid_json err=%v", sessionID, err)
			c.Data(http.StatusBadRequest, "application/json", anthropicErrorJSON("invalid_request_error", "invalid JSON"))
			return
		}

		// Simple estimate: count messages content length / 4
		messages, _ := raw["messages"].([]any)
		totalChars := 0
		for _, msg := range messages {
			if m, ok := msg.(map[string]any); ok {
				if content, ok := m["content"].(string); ok {
					totalChars += len(content)
				}
			}
		}
		tokenCount := totalChars / 4
		if tokenCount == 0 {
			tokenCount = 10 // minimum
		}

		log.Printf("[COUNT_TOKENS] session=%s estimated_tokens=%d", sessionID, tokenCount)

		resp := map[string]any{
			"input_tokens": tokenCount,
		}
		data, _ := json.Marshal(resp)
		c.Data(http.StatusOK, "application/json", data)
	}
}

// anthropicModelsHandler handles GET /v1/models with Anthropic models included
func anthropicModelsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// OpenAI format models
		openaiModels := []map[string]any{
			{"id": "minimax-m3", "object": "model", "created": 1748649600, "owned_by": "kimchi"},
			{"id": "kimi-k2.7", "object": "model", "created": 1748736000, "owned_by": "kimchi"},
			{"id": "deepseek-v4-flash", "object": "model", "created": 1748822400, "owned_by": "kimchi"},
		}

		// Anthropic models
		anthropicModels := []map[string]any{
			{"type": "model", "id": "claude-sonnet-4-20250514", "display_name": "Claude Sonnet 4", "created_at": "2025-05-14T00:00:00Z"},
			{"type": "model", "id": "claude-opus-4-20250514", "display_name": "Claude Opus 4", "created_at": "2025-05-14T00:00:00Z"},
			{"type": "model", "id": "claude-3-5-sonnet-20241022", "display_name": "Claude 3.5 Sonnet", "created_at": "2024-10-22T00:00:00Z"},
			{"type": "model", "id": "claude-3-5-haiku-20241022", "display_name": "Claude 3.5 Haiku", "created_at": "2024-10-22T00:00:00Z"},
			{"type": "model", "id": "claude-3-opus-20240229", "display_name": "Claude 3 Opus", "created_at": "2024-02-29T00:00:00Z"},
		}

		// Merge both formats
		allModels := make([]map[string]any, 0, len(openaiModels)+len(anthropicModels))
		allModels = append(allModels, openaiModels...)
		for _, m := range anthropicModels {
			allModels = append(allModels, map[string]any{
				"id":           m["id"],
				"object":       "model",
				"created":      1748649600,
				"owned_by":     "kimchi",
				"type":         m["type"],
				"display_name": m["display_name"],
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"object": "list",
			"data":   allModels,
		})
	}
}

// healthzHandler handles GET /healthz
func healthzHandler(c *gin.Context) {
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// truncateBody truncates a byte slice to maxLen for logging
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "... (truncated)"
}
