package config

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Redis         RedisConfig         `mapstructure:"redis"`
	VectorService VectorServiceConfig `mapstructure:"vector_service"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
	SemanticCache SemanticCacheConfig `mapstructure:"semantic_cache"`
	Providers     ProvidersConfig     `mapstructure:"providers"`
	Embedding     EmbeddingConfig     `mapstructure:"embedding"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type VectorServiceConfig struct {
	Addr      string `mapstructure:"addr"`
	TimeoutMS int    `mapstructure:"timeout_ms"`
}

type RateLimitConfig struct {
	TPM int64 `mapstructure:"tpm"` // tokens per minute
	RPM int64 `mapstructure:"rpm"` // requests per minute
}

type SemanticCacheConfig struct {
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
	TTLSeconds          int     `mapstructure:"ttl_seconds"`
}

type ProvidersConfig struct {
	OpenAIAPIKey   string `mapstructure:"openai_api_key"`
	GeminiAPIKey   string `mapstructure:"gemini_api_key"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type EmbeddingConfig struct {
	Provider   string `mapstructure:"provider"`
	Model      string `mapstructure:"model"`
	Dimensions int    `mapstructure:"dimensions"`
	Endpoint   string `mapstructure:"endpoint"`
	APIKey     string `mapstructure:"api_key"`
}

func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	_ = v.BindEnv("providers.openai_api_key", "OPENAI_API_KEY")
	_ = v.BindEnv("providers.gemini_api_key", "GEMINI_API_KEY")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")

	// default fallback values
	v.SetDefault("server.port", 8080)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("vector_service.addr", "localhost:50051")
	v.SetDefault("vector_service.timeout_ms", 3000)
	v.SetDefault("rate_limit.tpm", 10000)
	v.SetDefault("rate_limit.rpm", 60)
	v.SetDefault("semantic_cache.similarity_threshold", 0.85)
	v.SetDefault("semantic_cache.ttl_seconds", 3600)
	v.SetDefault("providers.timeout_seconds", 3)
	v.SetDefault("embedding.provider", "ollama")
	v.SetDefault("embedding.model", "all-minilm")
	v.SetDefault("embedding.dimensions", 384)
	v.SetDefault("embedding.endpoint", "http://localhost:11434/api/embeddings")

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundErr) {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if cfg.Providers.OpenAIAPIKey == "" {
		cfg.Providers.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	}
	if cfg.Providers.GeminiAPIKey == "" {
		cfg.Providers.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}

	return &cfg, nil
}
