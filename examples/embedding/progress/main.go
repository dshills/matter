// Example: using progress callbacks to build a custom UI.
//
// This demonstrates how to use SetProgressFunc to receive
// real-time events from the agent loop. You could use this
// to build a web UI, log to a file, send to a metrics system, etc.
//
// Build: go build ./examples/embedding/progress
// Run:   ./progress
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/runner"
	"github.com/dshills/matter/pkg/matter"
)

func main() {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 5

	client, err := llm.NewClient(llm.ProviderConfig{
		Provider: "mock",
		Model:    cfg.LLM.Model,
		Timeout:  cfg.LLM.Timeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	r, err := runner.New(cfg, client)
	if err != nil {
		log.Fatal(err)
	}

	// Register a progress callback that logs events as JSON.
	r.SetProgressFunc(func(event matter.ProgressEvent) {
		// Each event has: RunID, Step, Event, Data, Timestamp
		entry := map[string]any{
			"time":  event.Timestamp.Format(time.RFC3339),
			"step":  event.Step,
			"event": event.Event,
		}

		// Some events include extra data (tool name, error, etc.)
		if event.Data != nil {
			entry["data"] = event.Data
		}

		out, _ := json.Marshal(entry)
		fmt.Println(string(out))
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	result := r.Run(ctx, matter.RunRequest{
		Task:      "Analyze the project structure",
		Workspace: ".",
	})

	fmt.Fprintf(os.Stderr, "\nResult: success=%v steps=%d tokens=%d cost=$%.4f\n",
		result.Success, result.Steps, result.TotalTokens, result.TotalCostUSD)
}
