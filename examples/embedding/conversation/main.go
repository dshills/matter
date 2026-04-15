// Example: conversation mode with pause/resume.
//
// This demonstrates how to handle the agent's ask_user decision type
// programmatically. When the agent asks a question, the run pauses
// and you can provide an answer to resume it.
//
// Build: go build ./examples/embedding/conversation
// Run:   ./conversation
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/runner"
	"github.com/dshills/matter/pkg/matter"
)

func main() {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 20
	cfg.Agent.MaxAsks = 5 // allow up to 5 questions

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

	r.SetProgressFunc(func(event matter.ProgressEvent) {
		fmt.Fprintf(os.Stderr, "[step %d] %s\n", event.Step, event.Event)
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Run the task. If it pauses, handle the conversation loop.
	result := r.Run(ctx, matter.RunRequest{
		Task:      "Help me set up a new Go project",
		Workspace: ".",
	})

	scanner := bufio.NewScanner(os.Stdin)

	for result.Paused {
		// The agent is asking a question.
		fmt.Fprintf(os.Stderr, "\nAgent asks: %s\n", result.Question.Question)

		// Show options if provided.
		if len(result.Question.Options) > 0 {
			for i, opt := range result.Question.Options {
				fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt)
			}
		}

		// Read the user's answer.
		fmt.Fprint(os.Stderr, "Your answer: ")
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr, "EOF -- aborting")
			break
		}
		answer := strings.TrimSpace(scanner.Text())
		if answer == "" {
			continue
		}

		// Resume the run with the answer.
		result = r.Resume(ctx, answer)
	}

	// Print the final result.
	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
		os.Exit(1)
	}

	fmt.Printf("Success: %v\n", result.Success)
	fmt.Printf("Summary: %s\n", result.FinalSummary)
	fmt.Printf("Steps: %d\n", result.Steps)
}
