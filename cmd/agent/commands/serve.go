package commands

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	slackticketing "intern/internal/ticketing/slack"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// ServeCmd starts the agent as an HTTP server: a request-driven entrypoint
// (Slack webhook today) instead of the JIRA polling loop, and the mode
// Cloud Run deploys — a container that scales to zero between requests.
var ServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the AI Intern Agent over HTTP (Slack webhook, request-driven)",
	Long: `Start an HTTP server exposing the agent's request-driven trigger(s).
Currently supports the Slack Events API webhook (requires TICKETING_MODE=slack).
This is the entrypoint used for the Cloud Run deployment: it listens on $PORT,
handles one ask per request, and scales to zero when idle.`,
	RunE: serve,
}

func serve(cmd *cobra.Command, args []string) error {
	logger.Info("Starting AI Intern Agent (serve mode)...")

	deps, err := InitDependencies(context.Background())
	if err != nil {
		logger.Error("Failed to initialize dependencies: %v", err)
		return err
	}

	if deps.Config.TicketingMode != "slack" {
		return errors.New("agent serve requires TICKETING_MODE=slack (got " + deps.Config.TicketingMode + ")")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/slack/events", slackticketing.NewHandler(deps.SlackClient, deps.Config.SlackSigningSecret, deps.Coordinator))

	addr := ":" + deps.Config.Port
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received shutdown signal, shutting down HTTP server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
		cancel()
	}()

	logger.Info("Listening for Slack events", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("HTTP server failed: %v", err)
		return err
	}

	<-ctx.Done()
	return nil
}
