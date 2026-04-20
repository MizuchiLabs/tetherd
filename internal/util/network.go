// Package util contains utility functions
package util

import (
	"log/slog"
	"net"
)

// GetOutboundIP gets the preferred outbound ip of this machine
func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		slog.Warn(
			"Could not detect outbound IP automatically",
			"error",
			err,
		)
		return ""
	}
	defer func() { _ = conn.Close() }()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
