package client

import (
	"context"
	"log/slog"

	"github.com/mizuchilabs/tetherd/internal/util"
	"github.com/moby/moby/client"
)

type Watcher struct {
	dockerCLI *client.Client
	agentCLI  *Client
	hostIP    string
}

func NewWatcher(agentCLI *Client, hostIP string) (*Watcher, error) {
	dockerCLI, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	if hostIP == "" {
		hostIP = util.GetOutboundIP()
	}
	return &Watcher{
		dockerCLI: dockerCLI,
		agentCLI:  agentCLI,
		hostIP:    hostIP,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) {
	// Initial sync
	w.syncContainers(ctx)

	// Listen for events
	filters := client.Filters{}
	filters.Add("type", "container")
	filters.Add("event", "start")
	filters.Add("event", "die")
	filters.Add("event", "stop")
	filters.Add("event", "remove")
	filters.Add("event", "destroy")

	stream := w.dockerCLI.Events(ctx, client.EventsListOptions{Filters: filters})

	for {
		select {
		case <-ctx.Done():
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
