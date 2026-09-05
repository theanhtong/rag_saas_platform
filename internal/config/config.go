package config

import (
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
	TPM int64 `mapstructure:"tpm"` // Tokens per minute
	RPM int64 `mapstructure:"rpm"` // Requests per minute
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

func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Default fallback values
	v.SetDefault("server.port", 8080)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("vector_service.addr", "localhost:50051")
	v.SetDefault("vector_service.timeout_ms", 3000)
	v.SetDefault("rate_limit.tpm", 10000)
	v.SetDefault("rate_limit.rpm", 60)
	v.SetDefault("semantic_cache.similarity_threshold", 0.85)
	v.SetDefault("semantic_cache.ttl_seconds", 3600)
	v.SetDefault("providers.timeout_seconds", 3)

	if err := v.ReadInConfig(); err != nil {
		// If config file not found, proceed with default values and env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Ignore missing file error if we rely on env vars
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
