package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	return r
}

func TestAnthropicModelsHandler(t *testing.T) {
	r := setupTestRouter()
	r.GET("/v1/models", anthropicModelsHandler())

	req, _ := http.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["object"] != "list" {
		t.Errorf("expected object 'list', got %v", resp["object"])
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", resp["data"])
	}

	// Should have at least 3 OpenAI models + 5 Anthropic models
	if len(data) < 8 {
		t.Errorf("expected at least 8 models, got %d", len(data))
	}

	// Check for Anthropic model
	foundClaude := false
	for _, m := range data {
		model, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := model["id"].(string); ok && id == "claude-sonnet-4-20250514" {
			foundClaude = true
			break
		}
	}
	if !foundClaude {
		t.Error("expected to find claude-sonnet-4-20250514 in models list")
	}
}

func TestExtractAPIKey_XApiKey(t *testing.T) {
	r := setupTestRouter()
	r.GET("/test", func(c *gin.Context) {
		key := extractAPIKey(c)
		c.JSON(200, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("x-api-key", "test-key-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["key"] != "test-key-123" {
		t.Errorf("expected key 'test-key-123', got %v", resp["key"])
	}
}

func TestExtractAPIKey_BearerToken(t *testing.T) {
	r := setupTestRouter()
	r.GET("/test", func(c *gin.Context) {
		key := extractAPIKey(c)
		c.JSON(200, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer bearer-token-456")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["key"] != "bearer-token-456" {
		t.Errorf("expected key 'bearer-token-456', got %v", resp["key"])
	}
}

func TestExtractAPIKey_Missing(t *testing.T) {
	r := setupTestRouter()
	r.GET("/test", func(c *gin.Context) {
		key := extractAPIKey(c)
		c.JSON(200, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["key"] != "" {
		t.Errorf("expected empty key, got %v", resp["key"])
	}
}

func TestAnthropicMessagesHandler_MissingAPIKey(t *testing.T) {
	r := setupTestRouter()
	client := &http.Client{}
	r.POST("/v1/messages", anthropicMessagesHandler(client))

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	bodyBytes, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}

	var resp AnthropicErrorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error.Type != "authentication_error" {
		t.Errorf("expected error type 'authentication_error', got %v", resp.Error.Type)
	}
}

func TestAnthropicMessagesHandler_MissingModel(t *testing.T) {
	r := setupTestRouter()
	client := &http.Client{}
	r.POST("/v1/messages", anthropicMessagesHandler(client))

	body := map[string]any{
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "Hello"}},
	}
	bodyBytes, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var resp AnthropicErrorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error.Type != "invalid_request_error" {
		t.Errorf("expected error type 'invalid_request_error', got %v", resp.Error.Type)
	}
}

func TestHealthzHandler(t *testing.T) {
	r := setupTestRouter()
	r.GET("/healthz", healthzHandler)

	req, _ := http.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
}
