// Package config contains the application configuration.
package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/urfave/cli/v3"
)

type Config struct {
	Name        string
	Server      string
	Token       string
	Environment string
	HostIP      string
	Insecure    bool
}

// New reads the CLI flags and detects the host IP if it was not set.
func New(ctx context.Context, cmd *cli.Command) (*Config, error) {
	hostname, _ := os.Hostname()
	cfg := Config{
		Name:        cmp.Or(cmd.String("name"), hostname, "unknown"),
		Server:      cmd.String("server"),
		Token:       cmd.String("token"),
		Environment: cmd.String("environment"),
		HostIP:      cmd.String("host-ip"),
		Insecure:    cmd.Bool("insecure"),
	}
	if cfg.HostIP == "" {
		ip, err := outboundIP(ctx)
		if err != nil {
			return nil, fmt.Errorf("detecting host IP, set --host-ip: %w", err)
		}
		cfg.HostIP = ip
	}
	return &cfg, nil
}

// outboundIP returns the local address used to reach the internet. UDP dial sends no packets.
func outboundIP(ctx context.Context) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", errors.New("unexpected local address type")
	}
	return addr.IP.String(), nil
}
