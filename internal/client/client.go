// Package client contains the client implementation
package client

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"time"

	tetherv1 "github.com/mizuchilabs/tetherd/internal/gen/tether/v1"
	"github.com/mizuchilabs/tetherd/internal/gen/tether/v1/tetherv1connect"
)

type Client struct {
	name string
	env  string
	cli  tetherv1connect.AgentServiceClient
}

func NewClient(env, endpoint string, insecure bool) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		// #nosec - G402
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	// interceptors := connect.WithInterceptors(
	// 	withAuth(),
	// )

	hostname, _ := os.Hostname()
	client := &Client{name: hostname, env: env}
	client.cli = tetherv1connect.NewAgentServiceClient(
		httpClient,
		endpoint,
		// interceptors,
	)

	return client, nil
}

func (c *Client) Update(ctx context.Context, config []byte) {
	if _, err := c.cli.AgentHeartbeat(ctx, &tetherv1.AgentHeartbeatRequest{
		Name:   new(c.name),
		Env:    new(c.env),
		Config: config,
	}); err != nil {
		slog.Error("Failed to send heartbeat", "error", err)
	}
}
