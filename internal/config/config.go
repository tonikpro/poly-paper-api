package config

import (
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Port              int    `envconfig:"PORT" default:"8080"`
	DatabaseURL       string `envconfig:"DATABASE_URL" default:"postgres://poly:poly@localhost:5432/poly?sslmode=disable"`
	JWTSecret         string `envconfig:"JWT_SECRET" default:"dev-secret-change-me"`
	PolymarketCLOBURL string `envconfig:"POLYMARKET_CLOB_URL" default:"https://clob.polymarket.com"`
	PolymarketGammaURL string `envconfig:"POLYMARKET_GAMMA_URL" default:"https://gamma-api.polymarket.com"`
	EncryptionKey     string `envconfig:"ENCRYPTION_KEY" default:"dev-encryption-key-32bytes!!!!!!"` // 32 bytes for AES-256
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
