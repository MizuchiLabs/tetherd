package client

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/moby/moby/client"
)

const (
	// debounce batches bursts of events, e.g. a compose up starting many containers.
	debounce   = 250 * time.Millisecond
	retryDelay = 3 * time.Second
)

// Watch pushes a fresh config to updates whenever containers change, until ctx is done.
func Watch(ctx context.Context, docker *client.Client, hostIP string, updates chan []byte) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	var last []byte
	push := func() {
		data, err := buildConfig(ctx, docker, hostIP)
		if err != nil {
			slog.Error("Failed to build traefik config, retrying...", "error", err)
			timer.Reset(retryDelay)
			return
		}
		if bytes.Equal(data, last) {
			return
		}
		last = data
		// Replace any unsent config so only the latest is queued.
		select {
		case <-updates:
		default:
		}
		updates <- data
		slog.Debug("Config updated")
	}

	filters := client.Filters{}
	filters.Add("type", "container")
	filters.Add("event", "start", "die", "health_status: healthy", "health_status: unhealthy")

	for {
		res := docker.Events(ctx, client.EventsListOptions{Filters: filters})
		// Resync after every (re)subscribe so events missed while disconnected are caught.
		timer.Reset(0)

	stream:
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				push()
			case msg, ok := <-res.Messages:
				if !ok {
					break stream
				}
				slog.Debug("Docker event received", "action", msg.Action, "container", msg.Actor.ID)
				timer.Reset(debounce)
			case err := <-res.Err:
				if ctx.Err() != nil {
					return
				}
				slog.Error("Docker event stream failed, reconnecting...", "error", err)
				break stream
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func buildConfig(ctx context.Context, docker *client.Client, hostIP string) ([]byte, error) {
	filters := client.Filters{}
	filters.Add("label", "traefik.enable=true")
	res, err := docker.ContainerList(ctx, client.ContainerListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	return BuildTraefikConfig(res.Items, hostIP)
}
