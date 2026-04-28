// Package client contains the client implementation
package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mizuchilabs/tetherd/internal/config"
)

type HeartbeatRequest struct {
	Env    string          `json:"env"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewClient(cfg *config.Config) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		// #nosec - G402
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Update(ctx context.Context, config []byte) {
	reqBody := HeartbeatRequest{
		Name:   c.cfg.Hostname,
		Env:    c.cfg.Environment,
		Config: json.RawMessage(config),
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		slog.Error("Failed to marshal heartbeat request", "error", err)
		return
	}

	url := strings.TrimRight(c.cfg.Server, "/") + "/api/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		slog.Error("Failed to create heartbeat request", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// Generate the HMAC Signature
	if c.cfg.Token != "" {
		mac := hmac.New(sha256.New, []byte(c.cfg.Token))
		mac.Write(b)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Signature", signature)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("Failed to send heartbeat", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Heartbeat request failed", "status", resp.Status, "body", string(body))
	}
}
