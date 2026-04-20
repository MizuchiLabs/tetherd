package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mizuchilabs/tetherd/internal/agent"
	"github.com/mizuchilabs/tetherd/internal/api"
	"github.com/mizuchilabs/tetherd/internal/util"
	"github.com/urfave/cli/v3"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	cmd := &cli.Command{
		EnableShellCompletion: true,
		Suggest:               true,
		Name:                  "tetherd",
		Version:               fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, Date),
		Usage:                 "traefik agent for distributed nodes",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			hostIP := cmd.String("host-ip")
			port := cmd.String("port")
			auth := cmd.String("auth")

			level := slog.LevelInfo
			if cmd.Bool("debug") {
				level = slog.LevelDebug
			}
			slog.SetDefault(
				slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})),
			)

			if _, err := os.Stat("/var/run/docker.sock"); err != nil {
				slog.Warn("Docker socket not found", "path", "/var/run/docker.sock")
			}

			if hostIP == "" {
				hostIP = util.GetOutboundIP()
				if hostIP == "" {
					return errors.New(
						"could not detect outbound IP automatically, please set --host-ip manually",
					)
				}
				slog.Info("Host IP auto-detected", "ip", hostIP)
			}

			slog.Info("Starting tetherd agent", "version", Version, "host-ip", hostIP)

			// Initialize state manager
			state := agent.NewStateManager(hostIP)

			// Start Docker watcher
			watcher, err := agent.NewWatcher(state)
			if err != nil {
				return fmt.Errorf("failed to initialize docker watcher: %w", err)
			}

			go watcher.Start(ctx)

			// Start HTTP Server
			api.NewServer(port, auth, state).Start(ctx)
			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug logging",
				Sources: cli.EnvVars("TETHERD_DEBUG"),
			},
			&cli.StringFlag{
				Name:    "host-ip",
				Aliases: []string{"ip"},
				Usage:   "The public/routable IP of this host (used for Traefik routing). Auto-detected if empty.",
				Sources: cli.EnvVars("TETHERD_HOST_IP"),
			},
			&cli.StringFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Usage:   "Port for the traefik HTTP provider to listen on",
				Value:   "3000",
				Sources: cli.EnvVars("TETHERD_PORT"),
			},
			&cli.StringFlag{
				Name:    "auth",
				Aliases: []string{"a"},
				Usage:   "Basic auth credentials (e.g. 'user:password')",
				Sources: cli.EnvVars("TETHERD_AUTH"),
			},
		},
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cmd.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}
