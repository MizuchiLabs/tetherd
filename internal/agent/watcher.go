package agent

import (
	"context"
	"log/slog"

	"github.com/moby/moby/client"
)

type Watcher struct {
	cli   *client.Client
	state *StateManager
}

func NewWatcher(state *StateManager) (*Watcher, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		cli:   cli,
		state: state,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) {
	slog.Info("Starting Docker watcher...")

	// Do initial sync
	w.syncContainers(ctx)

	// Listen for events
	filters := client.Filters{}
	filters.Add("type", "container")
	filters.Add("event", "start")
	filters.Add("event", "die")
	filters.Add("event", "stop")
	filters.Add("event", "remove")
	filters.Add("event", "destroy")

	stream := w.cli.Events(ctx, client.EventsListOptions{Filters: filters})

	for {
		select {
		case <-ctx.Done():
			slog.Info("Docker watcher stopping")
			return
		case err := <-stream.Err:
			if err != nil {
				slog.Error("Docker event error", "error", err)
			}
		case msg := <-stream.Messages:
			slog.Debug("Docker event received", "action", msg.Action, "container", msg.Actor.ID)
			w.syncContainers(ctx)
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

	config, err := BuildTraefikConfig(containers.Items, w.state.GetHostIP())
	if err != nil {
		slog.Error("Failed to build Traefik config", "error", err)
		return
	}
	w.state.UpdateConfig(config)
}
