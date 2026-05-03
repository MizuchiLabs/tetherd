// Package client contains the client implementation
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/mizuchilabs/tetherd/internal/config"
)

type UpdateRequest struct {
	Env    string          `json:"env"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type Client struct {
	cfg          *config.Config
	latestUpdate []byte
}

func NewClient(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

// Connect starts the persistent connection and reconnect loop
func (c *Client) Connect(ctx context.Context) {
	url := strings.Replace(c.cfg.Server, "http", "ws", 1)
	url = strings.TrimRight(url, "/") + "/api/ws"

	for {
		err := c.handleConnection(ctx, url)
		if err != nil {
			slog.Error("WebSocket connection lost, retrying in 5s...", "error", err)
		}

		// Prevent tight looping on connection failure
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			// retry
		}
	}
}

func (c *Client) handleConnection(ctx context.Context, url string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: c.cfg.Insecure} // #nosec - G402
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	dialOptions := &websocket.DialOptions{
		HTTPClient: httpClient,
	}

	if c.cfg.Token != "" {
		dialOptions.HTTPHeader = http.Header{}
		dialOptions.HTTPHeader.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, _, err := websocket.Dial(dialCtx, url, dialOptions)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	// Read loop to detect disconnects and process control frames
	go func() {
		defer cancel()
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}()

	select {
	case latest := <-c.cfg.Updates:
		c.latestUpdate = latest
	default:
		// Channel is empty, proceed with existing cache
	}

	// Immediately push the last known state on reconnect
	if c.latestUpdate != nil {
		req := UpdateRequest{
			Name:   c.cfg.Hostname,
			Env:    c.cfg.Environment,
			Config: json.RawMessage(c.latestUpdate),
		}
		if err := wsjson.Write(ctx, conn, req); err != nil {
			return err // Server dropped us immediately, back to retry loop
		}
	}

	// Listen for config updates on the channel
	for {
		select {
		case <-ctx.Done():
			return conn.Close(websocket.StatusNormalClosure, "agent shutting down")
		case newConfig := <-c.cfg.Updates:
			c.latestUpdate = newConfig
			req := UpdateRequest{
				Name:   c.cfg.Hostname,
				Env:    c.cfg.Environment,
				Config: json.RawMessage(c.latestUpdate),
			}

			if err = wsjson.Write(ctx, conn, req); err != nil {
				return err // Returns to the reconnect loop
			}
		}
	}
}
