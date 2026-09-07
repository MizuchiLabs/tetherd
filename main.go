package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mizuchilabs/kata/buildinfo"
	"github.com/mizuchilabs/kata/logx"
	"github.com/mizuchilabs/kata/sigx"
	"github.com/urfave/cli/v3"

	"github.com/mizuchilabs/tetherd/internal/client"
	"github.com/mizuchilabs/tetherd/internal/config"
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
		Version:               buildinfo.String(),
		Usage:                 "traefik agent for distributed nodes",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			logx.Init(cmd.Bool("debug"))
			if _, err := os.Stat("/var/run/docker.sock"); err != nil {
				slog.Warn("Docker socket not found", "path", "/var/run/docker.sock")
			}

			cfg, err := config.New(ctx, cmd)
			if err != nil {
				return fmt.Errorf("failed to initialize config: %w", err)
			}

			cli := client.NewClient(cfg)
			watcher, err := client.NewWatcher(cfg)
			if err != nil {
				return fmt.Errorf("failed to initialize docker watcher: %w", err)
			}

			slog.Info("Starting tetherd", "version", Version)
			go cli.Connect(ctx)
			go watcher.Start(ctx)
			<-ctx.Done()

			slog.Info("Shutting down...")
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
				Name:    "server",
				Aliases: []string{"s"},
				Usage:   "The URL of the central Tether server",
				Value:   "http://127.0.0.1:3000",
				Sources: cli.EnvVars("TETHERD_SERVER"),
			},
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "The central Tether server authentication token",
				Sources: cli.EnvVars("TETHERD_TOKEN"),
			},
			&cli.StringFlag{
				Name:    "environment",
				Aliases: []string{"env"},
				Usage:   "The isolated environment group to send updates to",
				Value:   "default",
				Sources: cli.EnvVars("TETHERD_ENVIRONMENT"),
			},
		},
	}

	if err := cmd.Run(sigx.NotifyContext(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmd.Name, err)
		os.Exit(1)
	}
}
