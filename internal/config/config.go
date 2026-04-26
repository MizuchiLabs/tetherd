// Package config contains the application configuration
package config

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"
)

type Config struct {
	Hostname    string
	Server      string
	Token       string
	Environment string
	Insecure    bool
	Debug       bool
	Version     string
}

// New loads configuration from environment variables
func New(ctx context.Context, cmd *cli.Command) (*Config, error) {
	cfg := Config{}

	cfg.Hostname, _ = os.Hostname()
	if cfg.Hostname == "" {
		cfg.Hostname = "unknown"
	}
	cfg.Version = cmd.Root().Version
	cfg.Debug = cmd.Bool("debug")
	cfg.Insecure = cmd.Bool("insecure")
	cfg.Server = cmd.String("server")
	cfg.Environment = cmd.String("env")
	cfg.Token = cmd.String("token")

	return &cfg, nil
}
