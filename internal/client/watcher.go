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
	dockerCLI *client.Client
	agentCLI  *Client
	hostIP    string
	interval  time.Duration
}

func NewWatcher(agentCLI *Client, cfg *config.Config) (*Watcher, error) {
	dockerCLI, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}

	return &Watcher{
		dockerCLI: dockerCLI,
		agentCLI:  agentCLI,
		hostIP:    cfg.HostIP,
		interval:  cfg.Interval,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) {
	slog.Info("Starting Docker watcher...")

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Initial sync
	w.syncContainers(ctx)

	var stream <-chan events.Message
	var errs <-chan error

	startStream := func() {
		filters := client.Filters{}
		filters.Add("type", "container")
		filters.Add("event", "start")
		filters.Add("event", "die")
		filters.Add("event", "stop")
		filters.Add("event", "remove")
		filters.Add("event", "destroy")

		res := w.dockerCLI.Events(ctx, client.EventsListOptions{Filters: filters})
		stream = res.Messages
		errs = res.Err
	}

	startStream()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Docker watcher stopping")
			return
		case <-ticker.C:
			w.syncContainers(ctx)
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
			w.syncContainers(ctx)
		}
	}
}

func (w *Watcher) syncContainers(ctx context.Context) {
	filters := client.Filters{}
	filters.Add("label", "traefik.enable=true")
	containers, err := w.dockerCLI.ContainerList(
		ctx,
		client.ContainerListOptions{All: false, Filters: filters},
	)
	if err != nil {
		slog.Error("Failed to list containers", "error", err)
		return
	}

	config, err := BuildTraefikConfig(containers.Items, w.hostIP)
	if err != nil {
		slog.Error("Failed to build Traefik config", "error", err)
		return
	}

	w.agentCLI.Update(ctx, config)
}
