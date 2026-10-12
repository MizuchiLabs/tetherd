package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthy(t *testing.T) {
	heartbeatFile = filepath.Join(t.TempDir(), "tetherd.alive")

	require.Error(t, Healthy(), "no heartbeat yet")

	beat()
	require.NoError(t, Healthy(), "fresh heartbeat")

	stale := time.Now().Add(-4 * pingInterval)
	require.NoError(t, os.Chtimes(heartbeatFile, stale, stale))
	require.Error(t, Healthy(), "stale heartbeat")
}
