// Package config contains the application configuration
package config

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/mizuchilabs/tetherd/internal/util"
)

type Config struct {
	Hostname    string
	Server      string
	Token       string
	Environment string
	HostIP      string
	Insecure    bool
	Debug       bool
	Updates     chan []byte
}

// New loads configuration from environment variables.
func New(ctx context.Context, cmd *cli.Command) (*Config, error) {
	cfg := Config{}

	cfg.Hostname, _ = os.Hostname()
	if cfg.Hostname == "" {
		cfg.Hostname = "unknown"
	}
	cfg.HostIP = cmd.String("host-ip")
	if cfg.HostIP == "" {
		cfg.HostIP = util.GetOutboundIP(ctx)
	}
	cfg.Debug = cmd.Bool("debug")
	cfg.Insecure = cmd.Bool("insecure")
	cfg.Server = cmd.String("server")
	cfg.Environment = cmd.String("env")
	cfg.Token = cmd.String("token")
	cfg.Updates = make(chan []byte, 1)

	return &cfg, nil
}
