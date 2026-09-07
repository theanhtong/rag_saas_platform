package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/theanhtong/rag_system/internal/config"
)

// EmbeddingService generates vector embeddings for text chunks and queries.
type EmbeddingService interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

type embeddingService struct {
	cfg *config.EmbeddingConfig
}

// NewEmbeddingService constructs a new EmbeddingService instance.
func NewEmbeddingService(cfg *config.EmbeddingConfig) EmbeddingService {
	if cfg == nil {
		cfg = &config.EmbeddingConfig{
			Provider:   "ollama",
			Model:      "all-minilm",
			Dimensions: 384,
			Endpoint:   "http://localhost:11434/api/embeddings",
		}
	}
	return &embeddingService{cfg: cfg}
}

func (e *embeddingService) Dimensions() int {
	if e.cfg.Dimensions <= 0 {
		return 384
	}
	return e.cfg.Dimensions
}

func (e *embeddingService) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return make([]float32, e.Dimensions()), nil
	}

	switch strings.ToLower(e.cfg.Provider) {
	case "ollama":
		return e.embedOllama(ctx, text)
	case "openai":
		return e.embedOpenAI(ctx, text)
	default:
		return e.embedFallback(text), nil
	}
}

func (e *embeddingService) embedOllama(ctx context.Context, text string) ([]float32, error) {
	endpoint := e.cfg.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/api/embeddings"
	}

	model := e.cfg.Model
	if model == "" {
		model = "all-minilm"
	}

	payload := map[string]interface{}{
		"model":  model,
		"prompt": text,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return e.embedFallback(text), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return e.embedFallback(text), nil
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return e.embedFallback(text), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return e.embedFallback(text), nil
	}

	var ollamaResp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &ollamaResp); err == nil && len(ollamaResp.Embedding) > 0 {
		return ollamaResp.Embedding, nil
	}

	return e.embedFallback(text), nil
}

func (e *embeddingService) embedOpenAI(ctx context.Context, text string) ([]float32, error) {
	endpoint := e.cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/embeddings"
	}

	model := e.cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}

	payload := map[string]interface{}{
		"input": text,
		"model": model,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return e.embedFallback(text), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return e.embedFallback(text), nil
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return e.embedFallback(text), nil
	}
	defer resp.Body.Close()

	var openAIResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err == nil && len(openAIResp.Data) > 0 {
		return openAIResp.Data[0].Embedding, nil
	}

	return e.embedFallback(text), nil
}

func (e *embeddingService) embedFallback(text string) []float32 {
	dims := e.Dimensions()
	vec := make([]float32, dims)
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
		idx := int(h % uint32(dims))
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
