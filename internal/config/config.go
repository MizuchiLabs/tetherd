// Package config contains the application configuration
package config

import (
	"context"
	"os"
	"time"

	"github.com/mizuchilabs/tetherd/internal/util"
	"github.com/urfave/cli/v3"
)

type Config struct {
	Hostname    string
	Server      string
	Token       string
	Environment string
	HostIP      string
	Insecure    bool
	Debug       bool
	Version     string
	Interval    time.Duration
}

// New loads configuration from environment variables
func New(ctx context.Context, cmd *cli.Command) (*Config, error) {
	cfg := Config{}

	cfg.Hostname, _ = os.Hostname()
	if cfg.Hostname == "" {
		cfg.Hostname = "unknown"
	}
	cfg.HostIP = cmd.String("host-ip")
	if cfg.HostIP == "" {
		cfg.HostIP = util.GetOutboundIP()
	}
	cfg.Version = cmd.Root().Version
	cfg.Debug = cmd.Bool("debug")
	cfg.Insecure = cmd.Bool("insecure")
	cfg.Server = cmd.String("server")
	cfg.Environment = cmd.String("env")
	cfg.Token = cmd.String("token")
	cfg.Interval = cmd.Duration("interval")

	return &cfg, nil
}
