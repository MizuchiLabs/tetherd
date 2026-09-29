// Package client watches docker and pushes the resulting traefik config to tether.
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/mizuchilabs/tetherd/internal/config"
)

const pingInterval = 15 * time.Second

type updateRequest struct {
	Env    string          `json:"env"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

// Connect keeps a connection to tether open until ctx is done, pushing every config from updates.
func Connect(ctx context.Context, cfg *config.Config, updates <-chan []byte) error {
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return fmt.Errorf("parsing server url: %w", err)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u = u.JoinPath("api", "ws")

	opts := &websocket.DialOptions{HTTPClient: &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure}, // #nosec G402 -- opt-in flag
	}}}
	if cfg.Token != "" {
		opts.HTTPHeader = http.Header{"Authorization": {"Bearer " + cfg.Token}}
	}

	var latest []byte
	for {
		if err := session(ctx, cfg, u.String(), opts, updates, &latest); err != nil && ctx.Err() == nil {
			slog.Error("Connection lost, retrying...", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(3+rand.IntN(4)) * time.Second): // #nosec G404 -- retry jitter
		}
	}
}

// session runs one connection. It resends the latest config first so tether is current after a reconnect.
func session(
	ctx context.Context,
	cfg *config.Config,
	wsURL string,
	opts *websocket.DialOptions,
	updates <-chan []byte,
	latest *[]byte,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	//nolint:bodyclose // coder/websocket manages the handshake response body itself, docs say never to close it
	conn, _, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	slog.Info("Connected to tether", "server", cfg.Server, "name", cfg.Name, "env", cfg.Environment)

	// Reading is needed for pings and close frames, tether never sends data.
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	send := func(data []byte) error {
		*latest = data
		return wsjson.Write(ctx, conn, updateRequest{Name: cfg.Name, Env: cfg.Environment, Config: data})
	}
	if *latest != nil {
		if err := send(*latest); err != nil {
			return err
		}
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return conn.Close(websocket.StatusNormalClosure, "agent shutting down")
		case <-ping.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, pingInterval)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return fmt.Errorf("ping: %w", err)
			}
		case data := <-updates:
			if err := send(data); err != nil {
				return err
			}
		}
	}
}
