// Package util contains utility functions
package util

import (
	"context"
	"log/slog"
	"net"
)

func GetOutboundIP(ctx context.Context) string {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "udp", "8.8.8.8:80")
	if err != nil {
		slog.Warn("Could not detect outbound IP automatically", "error", err)
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		slog.Warn("Could not detect outbound IP automatically", "error", "unexpected local address type")
		return "127.0.0.1"
	}
	return localAddr.IP.String()
}
