package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mizuchilabs/tetherd/internal/client"
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
			endpoint := cmd.String("endpoint")
			env := cmd.String("env")

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

			cli, err := client.NewClient(env, endpoint, false)
			if err != nil {
				return fmt.Errorf("failed to initialize docker client: %w", err)
			}

			// Start Docker watcher
			watcher, err := client.NewWatcher(cli, hostIP)
			if err != nil {
				return fmt.Errorf("failed to initialize docker watcher: %w", err)
			}

			go watcher.Start(ctx)
			<-ctx.Done()
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
				Name:    "endpoint",
				Aliases: []string{"e"},
				Usage:   "The endpoint to send updates to. Default: http://127.0.0.1:3000",
				Value:   "http://127.0.0.1:3000",
				Sources: cli.EnvVars("TETHERD_ENDPOINT"),
			},
			&cli.StringFlag{
				Name:    "environment",
				Aliases: []string{"env"},
				Usage:   "The environment to send updates to. Default: default",
				Value:   "default",
				Sources: cli.EnvVars("TETHERD_ENVIRONMENT"),
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
