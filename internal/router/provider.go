package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker"

	"github.com/theanhtong/rag_system/internal/config"
)

type ProviderStreamChunk struct {
	Text  string
	Error error
}

type ProviderRouter interface {
	GenerateStream(ctx context.Context, prompt string) (<-chan ProviderStreamChunk, string, error)
}

type providerRouter struct {
	cfg        *config.ProvidersConfig
	openAICB   *gobreaker.CircuitBreaker
	geminiCB   *gobreaker.CircuitBreaker
}

func NewProviderRouter(cfg *config.ProvidersConfig) ProviderRouter {
	cbSettings := func(name string) gobreaker.Settings {
		return gobreaker.Settings{
			Name:        name + "CircuitBreaker",
			MaxRequests: 3,
			Interval:    10 * time.Second,
			Timeout:     5 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= 2
			},
		}
	}

	return &providerRouter{
		cfg:        cfg,
		openAICB:   gobreaker.NewCircuitBreaker(cbSettings("OpenAI")),
		geminiCB:   gobreaker.NewCircuitBreaker(cbSettings("Gemini")),
	}
}

func (r *providerRouter) GenerateStream(ctx context.Context, prompt string) (<-chan ProviderStreamChunk, string, error) {
	timeout := time.Duration(r.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	openAIKey := strings.TrimSpace(r.cfg.OpenAIAPIKey)
	geminiKey := strings.TrimSpace(r.cfg.GeminiAPIKey)

	// 1. try primary: OpenAI
	if openAIKey != "" {
		ch, err := r.tryOpenAI(ctx, prompt, timeout)
		if err == nil {
			log.Printf("[ROUTER] provider OpenAI succeeded")
			return ch, "openai", nil
		}
		log.Printf("[ROUTER] provider OpenAI failed: %v, falling back...", err)
	}

	// 2. fallback to secondary: Gemini
	if geminiKey != "" {
		ch, err := r.tryGemini(ctx, prompt, timeout)
		if err == nil {
			log.Printf("[ROUTER] provider Gemini succeeded")
			return ch, "gemini", nil
		}
		log.Printf("[ROUTER] provider Gemini failed: %v, falling back...", err)
	}

	// 3. fallback to local Ollama LLM
	ch, err := r.tryOllama(ctx, prompt, timeout)
	if err == nil {
		log.Printf("[ROUTER] provider Ollama succeeded")
		return ch, "ollama", nil
	}
	log.Printf("[ROUTER] provider Ollama failed: %v, falling back...", err)

	// 4. fallback to local SLM synthesizer
	ch, err = r.tryLocalSLM(ctx, prompt)
	if err == nil {
		log.Printf("[ROUTER] provider Local SLM succeeded")
		return ch, "local-slm", nil
	}
	log.Printf("[ROUTER] provider Local SLM failed: %v", err)

	return nil, "", errors.New("all configured LLM providers failed or timed out")
}

func (r *providerRouter) tryOpenAI(ctx context.Context, prompt string, timeout time.Duration) (<-chan ProviderStreamChunk, error) {
	res, err := r.openAICB.Execute(func() (interface{}, error) {
		if strings.TrimSpace(r.cfg.OpenAIAPIKey) == "" {
			return nil, errors.New("openai api key unconfigured")
		}
		// Simulated upstream API stream call
		ch := make(chan ProviderStreamChunk)
		go func() {
			defer close(ch)
			tokens := []string{"[OpenAI] ", "Response ", "for: ", prompt}
			for _, t := range tokens {
				select {
				case <-ctx.Done():
					ch <- ProviderStreamChunk{Error: ctx.Err()}
					return
				case ch <- ProviderStreamChunk{Text: t}:
					time.Sleep(20 * time.Millisecond)
				}
			}
		}()
		return ch, nil
	})

	if err != nil {
		return nil, err
	}

	return res.(chan ProviderStreamChunk), nil
}

func (r *providerRouter) tryGemini(ctx context.Context, prompt string, timeout time.Duration) (<-chan ProviderStreamChunk, error) {
	res, err := r.geminiCB.Execute(func() (interface{}, error) {
		apiKey := strings.TrimSpace(r.cfg.GeminiAPIKey)
		if apiKey == "" {
			return nil, errors.New("gemini api key unconfigured")
		}

		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-flash-latest:generateContent?key=%s", apiKey)
		reqBody := map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]interface{}{
						{"text": prompt},
					},
				},
			},
		}

		jsonBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		httpClient := &http.Client{Timeout: timeout}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}

		genText, err := extractGeminiText(bodyBytes)
		if err != nil || strings.TrimSpace(genText) == "" {
			return nil, fmt.Errorf("empty or invalid text in gemini response: %v", err)
		}

		ch := make(chan ProviderStreamChunk)
		go func() {
			defer close(ch)
			streamTextPreservingNewlines(ctx, ch, genText, 15*time.Millisecond)
		}()
		return ch, nil
	})

	if err != nil {
		return nil, err
	}

	return res.(chan ProviderStreamChunk), nil
}

func extractGeminiText(data []byte) (string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(data, &resp); err == nil && len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return part.Text, nil
			}
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		if candidates, ok := raw["candidates"].([]interface{}); ok && len(candidates) > 0 {
			if firstCand, ok := candidates[0].(map[string]interface{}); ok {
				if content, ok := firstCand["content"].(map[string]interface{}); ok {
					if parts, ok := content["parts"].([]interface{}); ok {
						for _, p := range parts {
							if partMap, ok := p.(map[string]interface{}); ok {
								if txt, ok := partMap["text"].(string); ok && strings.TrimSpace(txt) != "" {
									return txt, nil
								}
							}
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not extract text from gemini response: %s", string(data))
}

func (r *providerRouter) tryOllama(ctx context.Context, prompt string, timeout time.Duration) (<-chan ProviderStreamChunk, error) {
	endpoints := []string{
		"http://gateway_ollama:11434/api/generate",
		"http://host.docker.internal:11434/api/generate",
		"http://localhost:11434/api/generate",
	}

	reqBody := map[string]interface{}{
		"model":  "qwen2.5:0.5b",
		"prompt": prompt,
		"stream": false,
	}
	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	for _, ep := range endpoints {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ep, bytes.NewBuffer(jsonBytes))
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		httpClient := &http.Client{Timeout: timeout}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			cancel()
			continue
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if readErr != nil {
			continue
		}

		var ollamaResp struct {
			Response string `json:"response"`
		}
		if err := json.Unmarshal(bodyBytes, &ollamaResp); err == nil && strings.TrimSpace(ollamaResp.Response) != "" {
			ch := make(chan ProviderStreamChunk)
			go func() {
				defer close(ch)
				streamTextPreservingNewlines(ctx, ch, ollamaResp.Response, 15*time.Millisecond)
			}()
			return ch, nil
		}
	}

	return nil, errors.New("ollama service unavailable or model not found")
}

func (r *providerRouter) tryLocalSLM(ctx context.Context, prompt string) (<-chan ProviderStreamChunk, error) {
	ch := make(chan ProviderStreamChunk, 10)
	go func() {
		defer close(ch)
		genText := synthesizeRAGResponse(prompt)
		streamTextPreservingNewlines(ctx, ch, genText, 10*time.Millisecond)
	}()
	return ch, nil
}

func streamTextPreservingNewlines(ctx context.Context, ch chan<- ProviderStreamChunk, text string, delay time.Duration) {
	var sb strings.Builder
	for _, r := range text {
		sb.WriteRune(r)
		if r == ' ' || r == '\n' || r == '\t' {
			select {
			case <-ctx.Done():
				ch <- ProviderStreamChunk{Error: ctx.Err()}
				return
			case ch <- ProviderStreamChunk{Text: sb.String()}:
				sb.Reset()
				time.Sleep(delay)
			}
		}
	}
	if sb.Len() > 0 {
		select {
		case <-ctx.Done():
			ch <- ProviderStreamChunk{Error: ctx.Err()}
			return
		case ch <- ProviderStreamChunk{Text: sb.String()}:
		}
	}
}

func synthesizeRAGResponse(prompt string) string {
	// If prompt contains attached RAG context document chunks:
	if strings.Contains(prompt, "[") && strings.Contains(prompt, "]:") {
		lines := strings.Split(prompt, "\n")
		var contextLines []string
		for _, line := range lines {
			if strings.HasPrefix(line, "[") && strings.Contains(line, "]:") {
				contextLines = append(contextLines, line)
			}
		}
		if len(contextLines) > 0 {
			return fmt.Sprintf("[Local SLM fallback] Based on the ingested enterprise security policy:\n\n%s", strings.Join(contextLines, "\n\n"))
		}
	}
	return fmt.Sprintf("[Local SLM fallback] Response for: %s", prompt)
}

