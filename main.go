package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mizuchilabs/kata/buildinfo"
	"github.com/mizuchilabs/kata/logx"
	"github.com/mizuchilabs/kata/sigx"
	dockerclient "github.com/moby/moby/client"
	"github.com/urfave/cli/v3"

	"github.com/mizuchilabs/tetherd/internal/client"
	"github.com/mizuchilabs/tetherd/internal/config"
)

func main() {
	cmd := &cli.Command{
		EnableShellCompletion: true,
		Suggest:               true,
		Name:                  "tetherd",
		Version:               buildinfo.String(),
		Usage:                 "traefik agent for distributed nodes",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			logx.Init(cmd.Bool("debug"))
			return ctx, nil
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.New(ctx, cmd)
			if err != nil {
				return err
			}

			docker, err := dockerclient.New(dockerclient.FromEnv)
			if err != nil {
				return fmt.Errorf("creating docker client: %w", err)
			}
			context.AfterFunc(ctx, func() { _ = docker.Close() })
			if _, err := docker.Ping(ctx, dockerclient.PingOptions{}); err != nil {
				return fmt.Errorf("docker is not reachable: %w", err)
			}

			slog.Info("Starting tetherd", "version", buildinfo.Version, "name", cfg.Name, "host_ip", cfg.HostIP)
			updates := make(chan []byte, 1)
			go client.Watch(ctx, docker, cfg.HostIP, updates)
			return client.Connect(ctx, cfg, updates)
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
				Name:    "name",
				Aliases: []string{"n"},
				Usage:   "Unique agent name shown in tether. Defaults to the hostname.",
				Sources: cli.EnvVars("TETHERD_NAME"),
			},
			&cli.BoolFlag{
				Name:    "insecure",
				Usage:   "Skip TLS verification when connecting to tether",
				Sources: cli.EnvVars("TETHERD_INSECURE"),
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
