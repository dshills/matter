// Command matter-server is the HTTP API server for the matter agent framework.
//
// Usage:
//
//	matter-server [--config path] [--listen :8080]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/server"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	listenAddr := flag.String("listen", "", "override listen address (e.g., :8080)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if *listenAddr != "" {
		cfg.Server.ListenAddr = *listenAddr
	}

	llmClient, err := createLLMClient(cfg)
	if err != nil {
		log.Fatalf("LLM client error: %v", err)
	}

	srv := server.New(cfg, llmClient)

	// Handle graceful shutdown on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	// Wait for signal or server error.
	select {
	case <-ctx.Done():
		log.Println("shutting down...")
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}

func loadConfig(path string) (config.Config, error) {
	var cfg config.Config
	var err error

	if path != "" {
		cfg, err = config.LoadFromFile(path)
		if err != nil {
			return cfg, fmt.Errorf("loading config: %w", err)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	cfg, err = config.ApplyEnv(cfg)
	if err != nil {
		return cfg, fmt.Errorf("applying env: %w", err)
	}

	if err := config.Validate(cfg); err != nil {
		return cfg, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func createLLMClient(cfg config.Config) (llm.Client, error) {
	return llm.NewClient(llm.ProviderConfig{
		Provider: cfg.LLM.Provider,
		APIKey:   cfg.LLM.APIKey,
		Model:    cfg.LLM.Model,
		BaseURL:  cfg.LLM.BaseURL,
		Timeout:  cfg.LLM.Timeout,
	})
}
