// Example: embedding matter as a library within the same module.
//
// This demonstrates how to use the matter runner programmatically
// from Go code within the matter module. Since the runner and
// supporting packages are under internal/, this only works for
// code inside the matter module (e.g., custom cmd/ binaries).
//
// Build: go build ./examples/embedding
// Run:   ./embedding (uses mock client, no API key needed)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/runner"
	"github.com/dshills/matter/pkg/matter"
)

func main() {
	// Load default config and customize.
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 10
	cfg.Agent.MaxAsks = 0 // disable conversation mode

	// Create an LLM client.
	// For real usage, set provider to "openai", "anthropic", etc.
	// and ensure the API key is available via environment variable.
	client, err := llm.NewClient(llm.ProviderConfig{
		Provider: "mock", // change to "openai" for real usage
		Model:    cfg.LLM.Model,
		Timeout:  cfg.LLM.Timeout,
		// APIKey is resolved from the credential chain:
		//   MATTER_LLM_API_KEY > provider-specific env var > config
	})
	if err != nil {
		log.Fatalf("Failed to create LLM client: %v", err)
	}

	// Create the runner.
	r, err := runner.New(cfg, client)
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	// Set up a progress callback for real-time events.
	r.SetProgressFunc(func(event matter.ProgressEvent) {
		fmt.Fprintf(os.Stderr, "[step %d] %s\n", event.Step, event.Event)
	})

	// Create a cancellable context for signal handling.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Execute the task.
	result := r.Run(ctx, matter.RunRequest{
		Task:      "List the files in the current directory",
		Workspace: ".",
	})

	// Handle the result.
	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
		os.Exit(1)
	}

	fmt.Printf("Success: %v\n", result.Success)
	fmt.Printf("Summary: %s\n", result.FinalSummary)
	fmt.Printf("Steps: %d, Tokens: %d, Cost: $%.4f\n",
		result.Steps, result.TotalTokens, result.TotalCostUSD)
}
