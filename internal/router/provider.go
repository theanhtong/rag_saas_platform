package router

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sony/gobreaker"

	"github.com/theanhtong/llm_api_gateway/internal/config"
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

	// 1. Try Primary: OpenAI
	ch, err := r.tryOpenAI(ctx, prompt, timeout)
	if err == nil {
		return ch, "openai", nil
	}

	// 2. Fallback to Secondary: Gemini
	ch, err = r.tryGemini(ctx, prompt, timeout)
	if err == nil {
		return ch, "gemini", nil
	}

	// 3. Fallback to Local SLM / Mock
	ch, err = r.tryLocalSLM(ctx, prompt)
	if err == nil {
		return ch, "local-slm", nil
	}

	return nil, "", errors.New("all LLM providers failed or timed out")
}

func (r *providerRouter) tryOpenAI(ctx context.Context, prompt string, timeout time.Duration) (<-chan ProviderStreamChunk, error) {
	res, err := r.openAICB.Execute(func() (interface{}, error) {
		if r.cfg.OpenAIAPIKey == "" {
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
		if r.cfg.GeminiAPIKey == "" {
			return nil, errors.New("gemini api key unconfigured")
		}
		ch := make(chan ProviderStreamChunk)
		go func() {
			defer close(ch)
			tokens := []string{"[Gemini] ", "Fallback ", "response: ", prompt}
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

func (r *providerRouter) tryLocalSLM(ctx context.Context, prompt string) (<-chan ProviderStreamChunk, error) {
	ch := make(chan ProviderStreamChunk, 10)
	go func() {
		defer close(ch)
		words := strings.Fields("Local SLM fallback generated response for " + prompt)
		for _, w := range words {
			select {
			case <-ctx.Done():
				ch <- ProviderStreamChunk{Error: ctx.Err()}
				return
			case ch <- ProviderStreamChunk{Text: w + " "}:
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
	return ch, nil
}
