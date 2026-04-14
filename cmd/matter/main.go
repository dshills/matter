// Command matter is the CLI entry point for the matter agent framework.
//
// Usage:
//
//	matter run    --task "..." [--workspace .] [--config path]
//	matter config [--config path]
//	matter tools  [--config path]
//	matter replay (placeholder — not yet implemented)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/internal/runner"
	"github.com/dshills/matter/pkg/matter"
	"gopkg.in/yaml.v3"
)

// Exit codes per spec.
const (
	exitSuccess   = 0
	exitFailure   = 1
	exitConfigErr = 2
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(exitConfigErr)
	}

	cmd := os.Args[1]
	switch cmd {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "config":
		os.Exit(cmdConfig(os.Args[2:]))
	case "tools":
		os.Exit(cmdTools(os.Args[2:]))
	case "replay":
		os.Exit(cmdReplay(os.Args[2:]))
	case "help", "--help", "-h":
		printUsage()
		os.Exit(exitSuccess)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(exitConfigErr)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: matter <command> [flags]

Commands:
  run      Execute an agent task
  config   Print effective configuration
  tools    List registered tools
  replay   Replay a recorded run (not yet implemented)
  help     Show this help message

Run "matter <command> --help" for command-specific flags.`)
}

// cmdRun executes the "run" command.
func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	task := fs.String("task", "", "Task description for the agent (required)")
	workspace := fs.String("workspace", ".", "Workspace directory")
	cfgPath := fs.String("config", "", "Path to config file (optional)")
	mock := fs.Bool("mock", false, "Use mock LLM client (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitConfigErr
	}

	if *task == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required")
		fs.Usage()
		return exitConfigErr
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %s\n", err)
		return exitConfigErr
	}

	client, err := createLLMClient(cfg, *mock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm client error: %s\n", err)
		return exitConfigErr
	}

	r, err := runner.New(cfg, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialization error: %s\n", err)
		return exitConfigErr
	}

	// Set up signal handling for graceful cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "Starting task: %s\n", *task)
	fmt.Fprintf(os.Stderr, "Workspace: %s\n\n", *workspace)

	result := r.Run(ctx, matter.RunRequest{
		Task:      *task,
		Workspace: *workspace,
	})

	// Print result to stdout as JSON.
	printResult(result)

	if !result.Success {
		return exitFailure
	}
	return exitSuccess
}

// cmdConfig prints the effective configuration.
func cmdConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Path to config file (optional)")

	if err := fs.Parse(args); err != nil {
		return exitConfigErr
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %s\n", err)
		return exitConfigErr
	}

	redacted := config.RedactConfig(cfg)
	data, err := yaml.Marshal(redacted)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal config: %s\n", err)
		return exitFailure
	}

	fmt.Print(string(data))
	return exitSuccess
}

// cmdTools lists all registered tools.
func cmdTools(args []string) int {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Path to config file (optional)")

	if err := fs.Parse(args); err != nil {
		return exitConfigErr
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %s\n", err)
		return exitConfigErr
	}

	// Mock client is fine here — we only need the tool registry, not LLM calls.
	mock := llm.NewMockClient(nil, nil)
	r, err := runner.New(cfg, mock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialization error: %s\n", err)
		return exitConfigErr
	}

	toolList := r.Tools()
	if len(toolList) == 0 {
		fmt.Println("No tools registered.")
		return exitSuccess
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSAFE\tSIDE EFFECT\tDESCRIPTION")
	for _, t := range toolList {
		_, _ = fmt.Fprintf(w, "%s\t%v\t%v\t%s\n", t.Name, t.Safe, t.SideEffect, t.Description)
	}
	_ = w.Flush()

	return exitSuccess
}

// cmdReplay is a placeholder for Phase 10.
func cmdReplay(_ []string) int {
	fmt.Fprintln(os.Stderr, "replay command is not yet implemented (planned for Phase 10)")
	return exitFailure
}

// createLLMClient returns the appropriate LLM client based on config and flags.
// The --mock flag is a shorthand that overrides the provider to "mock".
func createLLMClient(cfg config.Config, mock bool) (llm.Client, error) {
	provider := cfg.LLM.Provider
	if mock {
		provider = "mock"
	}

	apiKey := llm.ResolveAPIKey(provider, cfg.LLM.APIKey)

	return llm.NewClient(llm.ProviderConfig{
		Provider:     provider,
		Model:        cfg.LLM.Model,
		Timeout:      cfg.LLM.Timeout,
		APIKey:       apiKey,
		BaseURL:      cfg.LLM.BaseURL,
		ExtraHeaders: cfg.LLM.ExtraHeaders,
	})
}

// loadConfig loads configuration from file (if provided), applies env overlays,
// and validates the result. If no path is given, defaults are used.
func loadConfig(path string) (config.Config, error) {
	var cfg config.Config

	if path != "" {
		var err error
		cfg, err = config.LoadFromFile(path)
		if err != nil {
			return config.Config{}, err
		}
	} else {
		cfg = config.DefaultConfig()
	}

	cfg, err := config.ApplyEnv(cfg)
	if err != nil {
		return config.Config{}, err
	}

	if err := config.Validate(cfg); err != nil {
		return config.Config{}, err
	}

	return cfg, nil
}

// printResult writes the run result to stdout as JSON.
func printResult(result matter.RunResult) {
	type outputResult struct {
		Success      bool    `json:"success"`
		FinalSummary string  `json:"final_summary"`
		Steps        int     `json:"steps"`
		TotalTokens  int     `json:"total_tokens"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Error        string  `json:"error,omitempty"`
	}

	out := outputResult{
		Success:      result.Success,
		FinalSummary: result.FinalSummary,
		Steps:        result.Steps,
		TotalTokens:  result.TotalTokens,
		TotalCostUSD: result.TotalCostUSD,
	}
	if result.Error != nil {
		out.Error = result.Error.Error()
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
