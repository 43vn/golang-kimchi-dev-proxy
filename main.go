package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	upstreamURL = "https://llm.kimchi.dev/openai/v1/chat/completions"
	defaultAddr = ":18998"
)

var (
	port     = flag.String("port", "", "listen address (e.g. :18998 or 0.0.0.0:18998); falls back to SERVER_ADDR env, then :18998")
	addrFlag = flag.String("addr", "", "alias for --port")
	listenAddr = getEnv("SERVER_ADDR", defaultAddr)
	version    = "dev"
)

// getEnv returns env value or fallback
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// normalizeAddr accepts a bare port ("9000") and prefixes ":", otherwise
// returns the input unchanged.
func normalizeAddr(a string) string {
	if a == "" {
		return a
	}
	if strings.HasPrefix(a, ":") || strings.Contains(a, ":") {
		return a
	}
	return ":" + a
}

func flushCopy(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			wn, werr := dst.Write(buf[:n])
			total += int64(wn)
			if werr != nil {
				return total, werr
			}
			if f, ok := dst.(http.Flusher); ok {
				f.Flush()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func stripDoubleV1() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for strings.HasPrefix(path, "/v1/v1") {
			path = "/v1" + path[7:]
		}
		c.Request.URL.Path = path
		c.Next()
	}
}

func main() {
	flag.Parse()

	// Resolve listen address: --port / --addr > SERVER_ADDR env > :18998
	switch {
	case *port != "":
		listenAddr = normalizeAddr(*port)
	case *addrFlag != "":
		listenAddr = normalizeAddr(*addrFlag)
	}

	log.Printf("Starting kimchi %s on %s", version, listenAddr)

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    100,
			IdleConnTimeout: 90 * time.Second,
		},
		Timeout: 0,
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(stripDoubleV1())

	r.GET("/v1/models", anthropicModelsHandler())

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read body"})
			return
		}
		c.Request.Body.Close()

		var payload map[string]any
		if err := json.Unmarshal(bodyBytes, &payload); err == nil {
			systemMsg := map[string]any{
				"role":    "developer",
				"content": "You are Kimchi, an AI coding agent",
			}
			if msgs, ok := payload["messages"].([]any); ok {
				payload["messages"] = append([]any{systemMsg}, msgs...)
			} else {
				payload["messages"] = []any{systemMsg}
			}
			bodyBytes, _ = json.Marshal(payload)
		}

		outReq, err := http.NewRequest(
			c.Request.Method,
			upstreamURL,
			io.NopCloser(bytes.NewReader(bodyBytes)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
			return
		}
		outReq = outReq.WithContext(c.Request.Context())

		outReq.Header.Set("Accept", "application/json")
		outReq.Header.Set("Content-Type", "application/json")
		outReq.Header.Set("User-Agent", "kimchi/0.1.52")
		outReq.Header.Set("Authorization", c.Request.Header.Get("Authorization"))
		outReq.Header.Set("X-Session-Id", uuid.NewString())
		outReq.Header.Set("X-Stainless-Arch", "x64")
		outReq.Header.Set("X-Stainless-Lang", "js")
		outReq.Header.Set("X-Stainless-OS", "Linux")
		outReq.Header.Set("X-Stainless-Package-Version", "6.26.0")
		outReq.Header.Set("X-Stainless-Retry-Count", "0")
		outReq.Header.Set("X-Stainless-Runtime", "node")
		outReq.Header.Set("X-Stainless-Runtime-Version", "v24.3.0")
		outReq.Header.Set("X-Stainless-Timeout", "300")
		outReq.Header.Set("X-Turn-Index", "0")

		for k, values := range outReq.Header {
			for _, v := range values {
				log.Printf("  %s: %s", k, v)
			}
		}
		resp, err := client.Do(outReq)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream failed: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		for k, values := range resp.Header {
			for _, v := range values {
				c.Writer.Header().Set(k, v)
			}
		}
		c.Writer.WriteHeader(resp.StatusCode)
		flushCopy(c.Writer, resp.Body)
	})

	// Anthropic Messages API routes
	r.POST("/v1/messages", anthropicMessagesHandler(client))
	r.POST("/v1/messages/count_tokens", anthropicCountTokensHandler(client))

	r.Run(listenAddr)
}
