package client

import (
	"context"
	"log/slog"
	"time"

	"github.com/mizuchilabs/tetherd/internal/config"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type Watcher struct {
	cli *client.Client
	cfg *config.Config
}

func NewWatcher(cfg *config.Config) (*Watcher, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}

	return &Watcher{
		cli: cli,
		cfg: cfg,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) {
	// Initial sync
	w.syncContainers(ctx)

	var stream <-chan events.Message
	var errs <-chan error

	startStream := func() {
		filters := client.Filters{}
		filters.Add("type", "container")
		filters.Add("event", "start")
		filters.Add("event", "die")
		filters.Add("event", "health_status: healthy")
		filters.Add("event", "health_status: unhealthy")

		res := w.cli.Events(ctx, client.EventsListOptions{Filters: filters})
		stream = res.Messages
		errs = res.Err
	}

	startStream()

	// Timer for debouncing rapid events (docker-compose)
	var debounceTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok || err != nil {
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					slog.Error("Docker event error", "error", err)
				}
				time.Sleep(3 * time.Second)
				startStream()
			}
		case msg, ok := <-stream:
			if !ok {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("Docker event stream closed, reconnecting...")
				time.Sleep(3 * time.Second)
				startStream()
				continue
			}
			slog.Debug("Docker event received", "action", msg.Action, "container", msg.Actor.ID)

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
				w.syncContainers(ctx)
			})
		}
	}
}

func (w *Watcher) syncContainers(ctx context.Context) {
	filters := client.Filters{}
	filters.Add("label", "traefik.enable=true")
	containers, err := w.cli.ContainerList(
		ctx,
		client.ContainerListOptions{All: false, Filters: filters},
	)
	if err != nil {
		slog.Error("Failed to list containers", "error", err)
		return
	}

	config, err := BuildTraefikConfig(containers.Items, w.cfg.HostIP)
	if err != nil {
		slog.Error("Failed to build Traefik config", "error", err)
		return
	}

	// Drain the channel to ensure we only queue the latest config
	select {
	case <-w.cfg.Updates:
	default:
	}

	select {
	case w.cfg.Updates <- config:
		slog.Debug("Config pushed to WebSocket channel")
	case <-ctx.Done():
		return
	default:
	}
}
