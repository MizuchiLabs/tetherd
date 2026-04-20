// Package api contains the API server
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mizuchilabs/tetherd/internal/agent"
)

type Server struct {
	port  string
	auth  string
	state *agent.StateManager
}

func NewServer(port string, auth string, state *agent.StateManager) *Server {
	return &Server{
		port:  port,
		auth:  auth,
		state: state,
	}
}

func (s *Server) Start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		// Basic Auth check
		if s.auth != "" {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			expectedAuth := fmt.Sprintf("%s:%s", user, pass)
			if expectedAuth != s.auth {
				w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(s.state.GetConfig()); err != nil {
			slog.Error("Failed to write response", "error", err)
		}
	})

	server := &http.Server{
		Addr:              "0.0.0.0:" + s.port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MiB
	}

	go func() {
		slog.Info("Server listening on", "address", "http://127.0.0.1:"+s.port)
		if err := server.ListenAndServe(); err != nil {
			slog.Error("Server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down...")
}
