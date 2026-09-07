package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/theanhtong/rag_system/internal/cache"
	"github.com/theanhtong/rag_system/internal/client"
	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/router"
)

// ChatMessage represents an individual message in a chat conversation request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest defines the standard OpenAI-compatible request payload.
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatCompletionChunkChoice represents a choice object in a streaming chunk.
type ChatCompletionChunkChoice struct {
	Index int `json:"index"`
	Delta struct {
		Content string `json:"content,omitempty"`
		Role    string `json:"role,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// ChatCompletionChunk defines the structure of SSE streaming data frames.
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
}

// ChatHandler manages HTTP REST and Server-Sent Events (SSE) chat completion requests.
type ChatHandler struct {
	vectorClient   client.VectorClient
	semanticCache  cache.SemanticCache
	providerRouter router.ProviderRouter
	embedder       embedder.EmbeddingService
}

// NewChatHandler constructs a new ChatHandler instance.
func NewChatHandler(vc client.VectorClient, sc cache.SemanticCache, pr router.ProviderRouter, emb embedder.EmbeddingService) *ChatHandler {
	return &ChatHandler{
		vectorClient:   vc,
		semanticCache:  sc,
		providerRouter: pr,
		embedder:       emb,
	}
}

// HandleChatCompletions handles incoming POST /v1/chat/completions requests.
// it executes semantic cache lookups, gRPC vector retrieval, RAG prompt augmentation, and SSE streaming.
func (h *ChatHandler) HandleChatCompletions(c *gin.Context) {
	var req ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_payload",
				"message": "failed to parse json request payload",
			},
		})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "empty_messages",
				"message": "messages array must contain at least one message",
			},
		})
		return
	}

	lastPrompt := req.Messages[len(req.Messages)-1].Content
	var promptVector []float32
	var err error

	if h.embedder != nil {
		promptVector, err = h.embedder.EmbedText(c.Request.Context(), lastPrompt)
		if err != nil || len(promptVector) == 0 {
			promptVector = generatePromptEmbedding(lastPrompt)
		}
	} else {
		promptVector = generatePromptEmbedding(lastPrompt)
	}

	// 1. execute semantic prompt cache lookup
	if h.semanticCache != nil {
		if cachedHit, found, err := h.semanticCache.Get(c.Request.Context(), promptVector); err == nil && found {
			c.Header("X-Cache", "HIT")
			c.Header("X-Cache-Similarity", fmt.Sprintf("%.4f", cachedHit.Similarity))

			if req.Stream {
				h.streamCachedResponse(c, req.Model, cachedHit.Response)
			} else {
				c.JSON(http.StatusOK, gin.H{
					"id":      fmt.Sprintf("chatcmpl-cache-%d", time.Now().Unix()),
					"object":  "chat.completion",
					"created": time.Now().Unix(),
					"model":   req.Model,
					"choices": []gin.H{
						{
							"index": 0,
							"message": gin.H{
								"role":    "assistant",
								"content": cachedHit.Response,
							},
							"finish_reason": "stop",
						},
					},
				})
			}
			return
		}
	}

	c.Header("X-Cache", "MISS")

	// 2. RAG context retrieval via gRPC vector service
	finalPrompt := lastPrompt
	var ragCitations []string

	if h.vectorClient != nil {
		searchResp, err := h.vectorClient.SearchSimilarVectors(c.Request.Context(), promptVector, 3)
		if err == nil && searchResp != nil && len(searchResp.Results) > 0 {
			var sb strings.Builder
			for i, res := range searchResp.Results {
				content := strings.TrimSpace(res.Metadata["content"])
				filename := res.Metadata["filename"]
				if content != "" {
					if filename != "" {
						sb.WriteString(fmt.Sprintf("[%s]: %s\n", filename, content))
						ragCitations = append(ragCitations, fmt.Sprintf("[%d] %s", i+1, filename))
					} else {
						sb.WriteString(content + "\n")
					}
				}
			}
			if sb.Len() > 0 {
				sb.WriteString(lastPrompt)
				finalPrompt = sb.String()
			}
		}
	}

	if len(ragCitations) > 0 {
		c.Header("X-RAG-Context", "ATTACHED")
		c.Header("X-RAG-Citations", strings.Join(ragCitations, "; "))
	} else {
		c.Header("X-RAG-Context", "NONE")
	}

	// 3. delegate to multi-provider LLM router
	streamCh, providerUsed, err := h.providerRouter.GenerateStream(c.Request.Context(), finalPrompt)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"code":    "provider_error",
				"message": err.Error(),
			},
		})
		return
	}

	c.Header("X-Provider-Used", providerUsed)

	if req.Stream {
		h.streamLiveResponse(c, req.Model, streamCh, promptVector, lastPrompt)
	} else {
		h.respondNonStream(c, req.Model, streamCh, promptVector, lastPrompt)
	}
}

// streamLiveResponse proxies LLM stream chunks token-by-token directly to the client over Server-Sent Events.
func (h *ChatHandler) streamLiveResponse(c *gin.Context, model string, ch <-chan router.ProviderStreamChunk, vector []float32, prompt string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	var fullResponse strings.Builder
	reqID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	c.Stream(func(w io.Writer) bool {
		chunk, ok := <-ch
		if !ok {
			c.SSEvent("", " [DONE]")
			return false
		}

		if chunk.Error != nil {
			return false
		}

		fullResponse.WriteString(chunk.Text)

		payload := ChatCompletionChunk{
			ID:      reqID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: struct {
						Content string `json:"content,omitempty"`
						Role    string `json:"role,omitempty"`
					}{Content: chunk.Text},
				},
			},
		}

		data, _ := json.Marshal(payload)
		c.SSEvent("", " "+string(data))
		return true
	})

	// async cache-aside: persist complete response into SemanticCache
	go func(resp string) {
		if resp != "" && h.semanticCache != nil {
			_ = h.semanticCache.Set(context.Background(), vector, prompt, resp)
		}
	}(fullResponse.String())
}

// streamCachedResponse streams a cached prompt completion over SSE protocol.
func (h *ChatHandler) streamCachedResponse(c *gin.Context, model string, content string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	reqID := fmt.Sprintf("chatcmpl-hit-%d", time.Now().UnixNano())

	payload := ChatCompletionChunk{
		ID:      reqID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: struct {
					Content string `json:"content,omitempty"`
					Role    string `json:"role,omitempty"`
				}{Content: content},
			},
		},
	}

	data, _ := json.Marshal(payload)
	c.SSEvent("", " "+string(data))
	c.SSEvent("", " [DONE]")
}

// respondNonStream collects all response chunks and returns a non-streaming JSON response object.
func (h *ChatHandler) respondNonStream(c *gin.Context, model string, ch <-chan router.ProviderStreamChunk, vector []float32, prompt string) {
	var fullResponse strings.Builder
	var lastErr error
	for chunk := range ch {
		if chunk.Error != nil {
			lastErr = chunk.Error
		} else {
			fullResponse.WriteString(chunk.Text)
		}
	}

	if fullResponse.Len() == 0 && lastErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"code":    "provider_error",
				"message": lastErr.Error(),
			},
		})
		return
	}

	respStr := fullResponse.String()
	go func() {
		if h.semanticCache != nil {
			_ = h.semanticCache.Set(context.Background(), vector, prompt, respStr)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []gin.H{
			{
				"index": 0,
				"message": gin.H{
					"role":    "assistant",
					"content": respStr,
				},
				"finish_reason": "stop",
			},
		},
	})
}

// generatePromptEmbedding computes a 16-dimensional unit-normalized vector embedding based on prompt tokens.
func generatePromptEmbedding(text string) []float32 {
	vec := make([]float32, 16)
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return vec
	}

	for _, word := range words {
		word = strings.Trim(word, "!?,.:;\"'()")
		if len(word) == 0 {
			continue
		}
		var h uint32 = 2166136261
		for i := 0; i < len(word); i++ {
			h ^= uint32(word[i])
			h *= 16777619
		}
		idx := int(h % 16)
		vec[idx] += 1.0
	}

	var norm float64 = 0.0
	for _, val := range vec {
		norm += float64(val) * float64(val)
	}

	if norm > 0 {
		mag := float32(math.Sqrt(norm))
		for i := range vec {
			vec[i] /= mag
		}
	}

	return vec
}
