# Developer Tools Specification

## 1. Overview

matter's built-in tool set limits agents to single-file reads, full-file writes, URL fetches, and command execution. This is sufficient for simple tasks but inadequate for real development workflows in large codebases. Agents cannot search for patterns across files, make surgical edits without rewriting entire files, or interact with version control.

This specification adds three tool categories that close the gap with production coding agents:

1. **Code search tools** — find files by pattern and search content by regex
2. **Diff-based file editing tool** — apply targeted edits to specific regions of a file
3. **Git tools** — read repository state and perform common version control operations

## 2. Goals

1. **Code search**: agents can locate files and search for patterns without reading every file sequentially.
2. **Surgical edits**: agents can modify specific lines/blocks without rewriting entire files, reducing token usage and avoiding accidental changes to untouched code.
3. **Git integration**: agents can inspect repo state (status, diff, log, blame) and perform common operations (add, commit, branch, checkout) within the workspace.
4. **Consistency**: new tools follow the same patterns as existing built-in tools — same constructor signature, JSON Schema input validation, workspace confinement, hidden path restrictions, safety classification, and error handling.
5. **Safety**: all new tools enforce workspace confinement via `workspace.ResolvePath()`. Git tools operate only within the workspace repository. No tool can escape the workspace root.

## 3. Non-Goals

- IDE integration or language server protocol (LSP) support.
- Tree-sitter or AST-based code analysis (valuable but complex; deferred).
- GitHub/GitLab API integration (PR creation, issue management). These belong in MCP servers.
- Interactive rebase, merge conflict resolution, or other interactive git operations.
- Git push — too dangerous for autonomous agents; users push manually or via MCP.

## 4. Code Search Tools

### 4.1 `workspace_find` — Find Files by Pattern

Recursively lists files in the workspace matching a glob pattern. Returns relative paths, one per line.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "Glob pattern to match file paths (e.g., '**/*.go', 'internal/**/*_test.go')"
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of results to return. Default 100, max 500."
    }
  },
  "required": ["pattern"],
  "additionalProperties": false
}
```

**Behavior:**

- Uses `filepath.WalkDir` with `doublestar.Match` (or equivalent) for `**` glob support.
- Skips hidden directories (directories starting with `.`) unless listed in `AllowedHiddenPaths`.
- Skips common non-text directories: `node_modules`, `vendor`, `.venv`, `__pycache__`, `dist`, `build`, `.idea`, `.vscode`.
- Returns paths relative to workspace root, sorted lexicographically.
- Truncates results at `max_results` (default 100, capped at 500) with a notice: `[TRUNCATED: showing 100 of 342 matches]`.
- Returns `ToolResult{Error: ...}` if the pattern is invalid.

**Classification:** `Safe: true`, `SideEffect: false`

### 4.2 `workspace_grep` — Search File Content

Searches file content for a regex pattern. Returns matching lines with file paths and line numbers.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "Regular expression pattern to search for (Go regexp syntax)"
    },
    "path": {
      "type": "string",
      "description": "Relative directory or file path to search within. Defaults to workspace root."
    },
    "glob": {
      "type": "string",
      "description": "Optional glob filter for file names (e.g., '*.go', '*.ts'). Applied to base filename only."
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of matching lines to return. Default 100, max 500."
    },
    "context_lines": {
      "type": "integer",
      "description": "Number of context lines before and after each match. Default 0, max 5."
    }
  },
  "required": ["pattern"],
  "additionalProperties": false
}
```

**Behavior:**

- Compiles `pattern` as a Go `regexp.Regexp`. Returns `ToolResult{Error: ...}` if invalid.
- Walks `path` (default: workspace root) recursively, reading text files.
- Skips binary files (detected by null bytes in first 512 bytes).
- Skips the same hidden/generated directories as `workspace_find`.
- When `glob` is set, only files whose base name matches the glob are searched.
- Output format: `path/to/file.go:42: matched line content` (grep-style).
- With `context_lines > 0`, outputs `--` separators between groups, prefixing context lines with `-` and match lines with `:` (standard grep -C format).
- Truncates at `max_results` matching lines with a notice: `[TRUNCATED: showing N of M matches]`.
- Validates `path` via `workspace.ResolvePath()`.
- Individual files larger than 1 MB are skipped. If any files were skipped, a notice is appended to the output: `[NOTICE: N files skipped (>1MB)]`.

**Classification:** `Safe: true`, `SideEffect: false`

## 5. Diff-Based File Editing Tool

### 5.1 `workspace_edit` — Apply Targeted Edits

Replaces a specific text region in a file without rewriting the entire file. The agent specifies the exact text to find and its replacement.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Relative path to the file to edit"
    },
    "old_text": {
      "type": "string",
      "description": "Exact text to find in the file (must match exactly, including whitespace)"
    },
    "new_text": {
      "type": "string",
      "description": "Text to replace old_text with"
    }
  },
  "required": ["path", "old_text", "new_text"],
  "additionalProperties": false
}
```

**Behavior:**

- Reads the file, finds the first occurrence of `old_text` (exact byte match, not regex). Line endings are compared as-is (no normalization). The agent should use the same line endings present in the file.
- If `old_text` is not found, returns `ToolResult{Error: "old_text not found in file"}`.
- If `old_text` appears more than once, returns `ToolResult{Error: "old_text is ambiguous (found N occurrences). Provide more surrounding context to make it unique."}`. This prevents accidental edits to the wrong location.
- Replaces the first (and only) occurrence with `new_text`.
- Writes the result using the same atomic write strategy as `workspace_write` (temp file + sync + rename).
- Validates path via `workspace.ResolvePath()` and `workspace.CheckHiddenPath()`.
- The file must already exist; returns error if it does not.
- `old_text` and `new_text` must differ; returns error if identical.
- Maximum file size for editing: 2 MB. Files larger than this return an error.
- Returns a confirmation message: `"Edited path/to/file.go: replaced N bytes (old_text length) at line L"` where N is the byte length of `old_text` and L is the line number of the first line of the match.

**Classification:** `Safe: false`, `SideEffect: true`

**Why exact match, not regex or line numbers?**

- Line numbers are fragile — they shift after any prior edit in the same file.
- Regex replacements risk unintended matches.
- Exact text match is the pattern used by Claude Code, Codex, and Cursor. It is the most reliable approach for LLM-driven edits because the agent can quote the exact code it sees.

## 6. Git Tools

All git tools operate on the git repository at the workspace root. If the workspace is not inside a git repository, all git tools return `ToolResult{Error: "workspace is not in a git repository"}`.

Git tools discover the repo root lazily on first invocation using `git rev-parse --show-toplevel`, then cache the result for subsequent calls. This allows git tools to function even if the repository is initialized (via `command_exec` running `git init`) during the agent's session.

**Workspace confinement:** The discovered repo root must be identical to the workspace root. If the repo root is a parent of the workspace (e.g., workspace is `/repo/subdir` but the repo root is `/repo`), the git tools return `ToolResult{Error: "git repository root is outside workspace"}`. This prevents agents from accessing files outside the workspace via git commands. All git commands are executed with the workspace root as the working directory.

### 6.1 `git_status` — Repository Status

Returns the output of `git status --porcelain=v1` for machine-parseable status.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}
```

**Behavior:**

- No input parameters. Returns the full porcelain status output.
- Output is workspace-relative paths.
- Truncated at the existing `SandboxConfig.MaxOutputBytes` (default 1 MB) if the output is very large.

**Classification:** `Safe: true`, `SideEffect: false`

### 6.2 `git_diff` — Show Changes

Returns `git diff` output for staged or unstaged changes.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "staged": {
      "type": "boolean",
      "description": "If true, show staged changes (git diff --cached). Default false (unstaged)."
    },
    "path": {
      "type": "string",
      "description": "Optional: limit diff to a specific file path (relative to workspace)"
    }
  },
  "additionalProperties": false
}
```

**Behavior:**

- Runs `git diff` (or `git diff --cached` when `staged: true`).
- When `path` is set, validates via `workspace.ResolvePath()` and appends `-- <path>` to the git command.
- Output truncated at `SandboxConfig.MaxOutputBytes`.

**Classification:** `Safe: true`, `SideEffect: false`

### 6.3 `git_log` — Commit History

Returns recent commit history.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "max_count": {
      "type": "integer",
      "description": "Number of commits to show. Default 10, max 50."
    },
    "oneline": {
      "type": "boolean",
      "description": "If true, use --oneline format. Default true."
    },
    "path": {
      "type": "string",
      "description": "Optional: show history for a specific file path"
    }
  },
  "additionalProperties": false
}
```

**Behavior:**

- Runs `git log` with `--max-count=N`.
- Default format is `--oneline`. When `oneline: false`, uses `--format=medium`.
- When `path` is set, validates via `workspace.ResolvePath()` and appends `-- <path>`.
- Output truncated at `SandboxConfig.MaxOutputBytes`.

**Classification:** `Safe: true`, `SideEffect: false`

### 6.4 `git_blame` — Line Attribution

Shows per-line authorship for a file.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Relative path to the file to blame"
    }
  },
  "required": ["path"],
  "additionalProperties": false
}
```

**Behavior:**

- Validates path via `workspace.ResolvePath()`.
- Runs `git blame <path>`.
- Output truncated at `SandboxConfig.MaxOutputBytes`.

**Classification:** `Safe: true`, `SideEffect: false`

### 6.5 `git_add` — Stage Files

Stages files for commit.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "paths": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Relative paths to stage. Use [\".\"] to stage all changes."
    }
  },
  "required": ["paths"],
  "additionalProperties": false
}
```

**Behavior:**

- Each path is validated via `workspace.ResolvePath()`.
- The special path `"."` stages all changes (equivalent to `git add -A`).
- Runs `git add <paths...>`.
- Returns the output of the `git add` command. On success (no output from git), returns `"Staged N paths"` where N is the count of paths provided in the input.

**Classification:** `Safe: false`, `SideEffect: true`

### 6.6 `git_commit` — Create Commit

Creates a commit with the staged changes.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "message": {
      "type": "string",
      "description": "Commit message"
    }
  },
  "required": ["message"],
  "additionalProperties": false
}
```

**Behavior:**

- Runs `git commit -m <message>`.
- Returns the commit output (hash, summary).
- Returns error if there are no staged changes.
- Does **not** support `--amend`, `--no-verify`, or any other flags. These are intentionally excluded for safety.

**Classification:** `Safe: false`, `SideEffect: true`

### 6.7 `git_branch` — List or Create Branches

Lists branches or creates a new branch.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Name of the branch to create. Omit to list branches."
    },
    "checkout": {
      "type": "boolean",
      "description": "If true, switch to the branch after creating it. Default false."
    }
  },
  "additionalProperties": false
}
```

**Behavior:**

- When `name` is omitted: runs `git branch -a` and returns the output.
- When `name` is set: runs `git branch <name>` to create the branch.
- When `checkout: true`: runs `git checkout -b <name>` instead (create and switch).
- Branch names are validated: must not contain spaces, `..`, `~`, `^`, `:`, `\`, control characters, or start with `-`. Returns error for invalid names.
- Returns error if the branch already exists (when creating).

**Classification:** `Safe: false` (when creating/switching), `SideEffect: true`

### 6.8 `git_checkout` — Switch Branches or Restore Files

Switches to a branch or restores a file to its committed state.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "branch": {
      "type": "string",
      "description": "Branch name to switch to"
    },
    "path": {
      "type": "string",
      "description": "File path to restore to its committed state (discards uncommitted changes)"
    }
  },
  "oneOf": [
    {"required": ["branch"]},
    {"required": ["path"]}
  ],
  "additionalProperties": false
}
```

**Behavior:**

- Exactly one of `branch` or `path` must be provided (enforced by schema `oneOf`).
- When `branch` is set: runs `git checkout <branch>`.
- When `path` is set: validates via `workspace.ResolvePath()`, runs `git checkout -- <path>`. This is a destructive operation (discards uncommitted changes to the file).
- Returns error if the branch does not exist.

**Classification:** `Safe: false`, `SideEffect: true`

## 7. Configuration

### 7.1 Config Additions

New fields added to the existing `ToolsConfig` struct (see `internal/config/config.go` for existing fields including `AllowedHiddenPaths []string`):

```go
type ToolsConfig struct {
    // ... existing fields (EnableWorkspaceRead, EnableWorkspaceWrite,
    //     EnableWebFetch, EnableCommandExec, CommandAllowlist,
    //     WebFetchAllowedDomains, AllowedHiddenPaths, MCPServers) ...

    EnableWorkspaceFind bool `yaml:"enable_workspace_find"`
    EnableWorkspaceGrep bool `yaml:"enable_workspace_grep"`
    EnableWorkspaceEdit bool `yaml:"enable_workspace_edit"`
    EnableGit           bool `yaml:"enable_git"`
}
```

Git tools and search tools use the existing `SandboxConfig.MaxOutputBytes` (default 1,048,576 bytes / 1 MB) for output truncation, the same value used by `workspace_read` and `command_exec`.

The existing `AllowedHiddenPaths` field controls which hidden directories are accessible to `workspace_find` and `workspace_grep`, consistent with its use by `workspace_read` and `workspace_write`.

The existing `workspace.CheckHiddenPath(relPath string, allowedPaths []string)` function (defined in `internal/workspace/guard.go`) is used by `workspace_edit` to restrict writes to hidden paths, consistent with `workspace_write`.

**Defaults:**

- `EnableWorkspaceFind: true` — safe, read-only
- `EnableWorkspaceGrep: true` — safe, read-only
- `EnableWorkspaceEdit: false` — requires explicit opt-in (mutates files)
- `EnableGit: false` — requires explicit opt-in (mutates repository state)

### 7.2 Environment Variables

```
MATTER_TOOLS_ENABLE_WORKSPACE_FIND=true
MATTER_TOOLS_ENABLE_WORKSPACE_GREP=true
MATTER_TOOLS_ENABLE_WORKSPACE_EDIT=true
MATTER_TOOLS_ENABLE_GIT=true
```

## 8. Git Safety Constraints

Git tools enforce safety constraints to prevent agents from causing irreversible damage:

1. **No push.** There is no `git_push` tool. Pushing is a high-risk operation that should be done by the user or via MCP.
2. **No force operations.** `--force`, `--hard`, `--amend`, `--no-verify` are not exposed.
3. **No interactive operations.** Rebase, merge, cherry-pick are not supported.
4. **Branch name validation.** Prevents injection via crafted branch names.
5. **Workspace confinement.** All path arguments are validated via `workspace.ResolvePath()`. Git commands run with the repo root as the working directory — they cannot operate on repositories outside the workspace.
6. **Timeout enforcement.** Read-only git tools (`git_status`, `git_diff`, `git_log`, `git_blame`) use a 10-second timeout. Mutating git tools (`git_add`, `git_commit`, `git_branch`, `git_checkout`) use a 20-second timeout. These are hardcoded in the tool constructors, not configurable.

## 9. Dependency Policy

- **`doublestar`** (`github.com/bmatcuk/doublestar/v4`): Required for `**` glob support in `workspace_find`. Go's `filepath.Match` does not support `**`. Small, zero-dependency library.
- **`go-gitignore`** (`github.com/sabhiram/go-gitignore`): Required for `.gitignore` pattern matching in `workspace_find` and `workspace_grep`. Small, zero-dependency library.
- **No other new dependencies.** Git operations use `os/exec` to call the system `git` binary, matching the pattern established by `command_exec`.

## 10. Error Handling

All new tools follow the established error handling convention:

- **User-facing errors** (bad input, file not found, pattern invalid): `return matter.ToolResult{Error: msg}, nil`
- **Exceptional errors** (OS-level failures): `return matter.ToolResult{}, fmt.Errorf(...)`
- **Git not installed:** `git_*` tools check for `git` on PATH at construction time. If not found, the tool returns `ToolResult{Error: "git is not installed or not in PATH"}` on every call rather than failing at registration (consistent with graceful degradation).
