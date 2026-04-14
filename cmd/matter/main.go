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
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

	// Register progress callback for CLI stderr output.
	r.SetProgressFunc(cliProgressFunc(*workspace))

	// Set up signal handling for graceful cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	result := r.Run(ctx, matter.RunRequest{
		Task:      *task,
		Workspace: *workspace,
	})

	// Handle conversation mode: prompt for input when the agent asks.
	// Use a single scanner to avoid buffering data loss across calls.
	// Note: Scan() blocks on stdin which is standard CLI behavior. SIGINT
	// is handled by signal.NotifyContext which terminates the process.
	// Single-line input is sufficient for v1 CLI conversation mode.
	// Multi-line support can be added in a future version if needed.
	stdinScanner := bufio.NewScanner(os.Stdin)
	stdinScanner.Buffer(make([]byte, bufio.MaxScanTokenSize), 1024*1024) // up to 1MB
	for result.Paused {
		if result.Question == nil {
			fmt.Fprintln(os.Stderr, "\n(agent paused without a question, cancelling)")
			break
		}
		fmt.Fprintf(os.Stderr, "\nAgent question: %s\n", result.Question.Question)
		if len(result.Question.Options) > 0 {
			for i, opt := range result.Question.Options {
				fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt)
			}
		}
		fmt.Fprint(os.Stderr, "> ")
		answer, ok := scanLine(stdinScanner)
		if !ok {
			// EOF or read error — cancel the run.
			if err := stdinScanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "\n(input error: %v, cancelling)\n", err)
			} else {
				fmt.Fprintln(os.Stderr, "\n(EOF, cancelling)")
			}
			break
		}
		// Map numeric input to option string (e.g., "1" → first option).
		answer = resolveOption(answer, result.Question.Options)
		result = r.Resume(ctx, answer)
	}

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

// scanLine reads the next line from the scanner, trimming whitespace.
// TrimSpace is intentional: CLI answers are plain text where leading/trailing
// whitespace is never significant. Returns ("", false) on EOF or error.
// Callers should check scanner.Err() to distinguish EOF from read errors.
func scanLine(s *bufio.Scanner) (string, bool) {
	if s.Scan() {
		return strings.TrimSpace(s.Text()), true
	}
	return "", false
}

// resolveOption maps a numeric input string to the corresponding option.
// Exact string matches take priority over index parsing, so if an option
// is itself a number, the user can type it literally. If no exact match
// and the input is a valid 1-based index, returns the indexed option.
// Otherwise returns the input unchanged.
func resolveOption(input string, options []string) string {
	if len(options) == 0 {
		return input
	}
	// Prefer exact match over index lookup.
	for _, opt := range options {
		if input == opt {
			return input
		}
	}
	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || n > len(options) {
		return input
	}
	return options[n-1]
}

// cliProgressFunc returns a ProgressFunc that prints progress to stderr.
func cliProgressFunc(workspace string) matter.ProgressFunc {
	return func(e matter.ProgressEvent) {
		switch e.Event {
		case "run_started":
			fmt.Fprintf(os.Stderr, "Run %s started\n", e.RunID)
			if task, ok := e.Data["task"].(string); ok {
				fmt.Fprintf(os.Stderr, "Task: %s\n", task)
			}
			fmt.Fprintf(os.Stderr, "Workspace: %s\n\n", workspace)
		case "planner_started":
			fmt.Fprintf(os.Stderr, "  [step %d] planning...\n", e.Step)
		case "planner_completed":
			fmt.Fprintf(os.Stderr, "  [step %d] planner responded (tokens: %v, cost: $%v)\n",
				e.Step, e.Data["tokens"], e.Data["cost"])
		case "planner_failed":
			fmt.Fprintf(os.Stderr, "  [step %d] planner failed: %v\n", e.Step, e.Data["error"])
		case "tool_started":
			fmt.Fprintf(os.Stderr, "  [step %d] calling tool: %v\n", e.Step, e.Data["tool"])
		case "tool_completed":
			if errMsg, ok := e.Data["error"].(string); ok && errMsg != "" {
				fmt.Fprintf(os.Stderr, "  [step %d] tool %v failed (%v): %s\n",
					e.Step, e.Data["tool"], e.Data["duration"], errMsg)
			} else {
				fmt.Fprintf(os.Stderr, "  [step %d] tool %v completed (%v)\n",
					e.Step, e.Data["tool"], e.Data["duration"])
			}
		case "limit_exceeded":
			fmt.Fprintf(os.Stderr, "  [step %d] limit exceeded: %v — %v\n",
				e.Step, e.Data["limit"], e.Data["message"])
		case "run_completed":
			fmt.Fprintf(os.Stderr, "\nCompleted: %v (%d steps)\n", e.Data["success"], e.Step)
		}
	}
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
