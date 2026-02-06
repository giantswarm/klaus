package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/giantswarm/klaus/pkg/claude"
	"github.com/giantswarm/klaus/pkg/project"
	"github.com/giantswarm/klaus/pkg/server"
)

func main() {
	fmt.Printf("%s %s (build: %s, commit: %s)\n",
		project.Name, project.Version(), project.BuildTimestamp(), project.GitSHA())

	// Build Claude options from environment variables.
	opts := claude.DefaultOptions()

	if v := os.Getenv("CLAUDE_MODEL"); v != "" {
		opts.Model = v
	}
	if v := os.Getenv("CLAUDE_SYSTEM_PROMPT"); v != "" {
		opts.SystemPrompt = v
	}
	if v := os.Getenv("CLAUDE_APPEND_SYSTEM_PROMPT"); v != "" {
		opts.AppendSystemPrompt = v
	}
	if v := os.Getenv("CLAUDE_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.MaxTurns = n
		}
	}
	if v := os.Getenv("CLAUDE_PERMISSION_MODE"); v != "" {
		opts.PermissionMode = v
	}
	if v := os.Getenv("CLAUDE_MCP_CONFIG"); v != "" {
		opts.MCPConfigPath = v
	}
	if v := os.Getenv("CLAUDE_WORKSPACE"); v != "" {
		opts.WorkDir = v
	}

	// Create the Claude process manager.
	process := claude.NewProcess(opts)

	// Determine listen port.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create and start the HTTP server.
	srv := server.New(process, port)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Stop any running Claude process.
	if err := process.Stop(); err != nil {
		log.Printf("Error stopping Claude process: %v", err)
	}

	// Graceful HTTP shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
