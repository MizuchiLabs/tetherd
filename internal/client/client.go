// Package client contains the client implementation
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"math/rand"
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
		if err := c.handler(ctx, url); err != nil {
			slog.Error("Connection lost, retrying...", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(3+rand.Intn(4)) * time.Second): // #nosec - G404
			// retry with some jitter
		}
	}
}

func (c *Client) handler(ctx context.Context, url string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: c.cfg.Insecure} // #nosec - G402
	dialOptions := &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
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

	// read loop to detect disconnects
	go func() {
		defer cancel()
		conn.SetReadLimit(32768) // prevent memory exhaustion
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
	}

	// push the last known state on reconnect
	if c.latestUpdate != nil {
		if err := wsjson.Write(ctx, conn, UpdateRequest{
			Name:   c.cfg.Hostname,
			Env:    c.cfg.Environment,
			Config: json.RawMessage(c.latestUpdate),
		}); err != nil {
			return err // back to retry loop
		}
	}

	for {
		select {
		case <-ctx.Done():
			return conn.Close(websocket.StatusNormalClosure, "agent shutting down")
		case newConfig := <-c.cfg.Updates:
			c.latestUpdate = newConfig
			if err = wsjson.Write(ctx, conn, UpdateRequest{
				Name:   c.cfg.Hostname,
				Env:    c.cfg.Environment,
				Config: json.RawMessage(c.latestUpdate),
			}); err != nil {
				return err
			}
		}
	}
}
