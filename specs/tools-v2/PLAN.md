# Developer Tools — Implementation Plan

## Phase 1: Code Search Tools (`workspace_find` and `workspace_grep`)

### Goals

Add `workspace_find` (glob-based file listing) and `workspace_grep` (regex content search) as built-in tools. These are read-only, safe tools that require no new configuration flags (enabled by default).

### Files to Create

- `internal/tools/builtin/workspace_find.go` — `NewWorkspaceFind` constructor, `WorkspaceFindSchema`, glob walking logic
- `internal/tools/builtin/workspace_find_test.go` — glob pattern matching, hidden dir skipping, result truncation, invalid patterns
- `internal/tools/builtin/workspace_grep.go` — `NewWorkspaceGrep` constructor, `WorkspaceGrepSchema`, regex search with context lines
- `internal/tools/builtin/workspace_grep_test.go` — regex search, glob filtering, context lines, binary file skipping, large file skipping, truncation

### Files to Modify

- `go.mod` / `go.sum` — add `github.com/bmatcuk/doublestar/v4` for `**` glob support
- `internal/config/config.go` — add `EnableWorkspaceFind bool` and `EnableWorkspaceGrep bool` to `ToolsConfig`; add env var resolution for both
- `internal/config/defaults.go` — set both to `true`
- `internal/runner/runner.go` — register `workspace_find` and `workspace_grep` in `registerBuiltinTools` when enabled

### Key Decisions

- `doublestar` (`github.com/bmatcuk/doublestar/v4`) is needed for `**` glob support that Go's `filepath.Match` lacks.
- `go-gitignore` (`github.com/sabhiram/go-gitignore`) is used to parse `.gitignore` files and filter results during directory walks. If a `.gitignore` exists at the workspace root, its patterns are respected. Files matched by `.gitignore` are excluded from both `workspace_find` and `workspace_grep` results. Both are small, zero-dependency libraries.
- Both tools reuse the existing `AllowedHiddenPaths` config for hidden directory filtering.
- Both tools reuse `SandboxConfig.MaxOutputBytes` for output truncation.
- Skip list for non-text directories: `node_modules`, `vendor`, `.venv`, `__pycache__`, `dist`, `build`, `.idea`, `.vscode`. Defined as a package-level slice in `workspace_find.go` and shared by `workspace_grep.go`.
- Binary file detection in `workspace_grep`: check first 512 bytes for null bytes. Skip silently.
- `workspace_grep` appends `[NOTICE: N files skipped (>1MB)]` when large files are encountered.
- `workspace_find` and `workspace_grep` both accept `max_results` with default 100, max 500.
- Context lines in `workspace_grep`: max 5, output uses grep `-C` format (`--` separators, `-` for context, `:` for matches).

### Acceptance Criteria

- `workspace_find` with `"**/*.go"` returns all Go files, one per line, sorted.
- `workspace_find` skips `.git`, `node_modules`, and other excluded dirs.
- `workspace_find` with `AllowedHiddenPaths: [".github"]` includes `.github` contents.
- `workspace_find` truncates at `max_results` with notice.
- `workspace_find` returns error for invalid glob patterns.
- `workspace_grep` with `"func.*Handler"` returns matching lines with file:line: format.
- `workspace_grep` with `glob: "*.go"` restricts search to Go files.
- `workspace_grep` with `context_lines: 2` includes surrounding context.
- `workspace_grep` skips binary files (no crash, no output for binary).
- `workspace_grep` appends notice when large files are skipped.
- `workspace_grep` returns error for invalid regex.
- `workspace_grep` validates `path` via `workspace.ResolvePath()`.
- Both tools appear in `matter tools` output with correct safety flags.
- All existing tests pass.

---

## Phase 2: Diff-Based File Editing (`workspace_edit`)

### Goals

Add `workspace_edit` for surgical text replacement within files. This reduces token usage and avoids accidental changes compared to full-file rewrites via `workspace_write`.

### Files to Create

- `internal/tools/builtin/workspace_edit.go` — `NewWorkspaceEdit` constructor, `WorkspaceEditSchema`, exact-match find-and-replace logic
- `internal/tools/builtin/workspace_edit_test.go` — single match replacement, ambiguous match error, not found error, atomic write, hidden path restriction, file size limit, identical old/new error, line number reporting

### Files to Modify

- `internal/config/config.go` — add `EnableWorkspaceEdit bool` to `ToolsConfig`; add env var resolution
- `internal/config/defaults.go` — set `EnableWorkspaceEdit` to `false` (requires opt-in)
- `internal/runner/runner.go` — register `workspace_edit` in `registerBuiltinTools` when enabled

### Key Decisions

- Exact byte match (not regex). The agent quotes the exact text it wants to replace. Line endings are compared as-is (no normalization). This matches the behavior of Claude Code and Codex, which both require the agent to use the file's actual line endings. In practice, LLMs observe the file content (read via `workspace_read`) and reproduce its line endings in `old_text`.
- Ambiguity check: if `old_text` appears more than once, return error with occurrence count and guidance to provide more context. This is critical for correctness.
- Reuse the existing `atomicWrite` pattern from `workspace_write` (temp file + chmod + sync + rename). Extract it to a shared helper if not already shared, or duplicate the small function.
- Maximum file size for editing: 2 MB (hardcoded). Larger files return an error. This is intentionally conservative — LLMs cannot reliably produce exact-match text for files they haven't fully read, and reading files >2 MB would exceed most context windows. A future version could make this configurable.
- The confirmation message includes the line number of the match, helping the agent verify it edited the right location.
- `workspace_edit` enforces `workspace.ResolvePath()` and `workspace.CheckHiddenPath()`, matching `workspace_write` behavior.

### Acceptance Criteria

- Unique `old_text` is replaced with `new_text`; file content verified.
- `old_text` not found returns error with clear message.
- `old_text` found twice returns error with `"found 2 occurrences"`.
- `old_text == new_text` returns error.
- Atomic write: partial failures don't corrupt the file.
- Hidden path writes are blocked unless in `AllowedHiddenPaths`.
- Files > 2 MB return error.
- Nonexistent files return error.
- Confirmation includes line number.
- Tool appears in `matter tools` as `Safe: false, SideEffect: true`.
- All existing tests pass.

---

## Phase 3: Git Read Tools (`git_status`, `git_diff`, `git_log`, `git_blame`)

### Goals

Add read-only git tools that let the agent inspect repository state. These are safe tools that execute `git` as a subprocess.

### Files to Create

- `internal/tools/builtin/git.go` — shared git helper: lazy repo root discovery, git command runner, workspace confinement check, branch name validation
- `internal/tools/builtin/git_status.go` — `NewGitStatus` constructor and schema
- `internal/tools/builtin/git_diff.go` — `NewGitDiff` constructor and schema
- `internal/tools/builtin/git_log.go` — `NewGitLog` constructor and schema
- `internal/tools/builtin/git_blame.go` — `NewGitBlame` constructor and schema
- `internal/tools/builtin/git_test.go` — shared test helpers: temp git repo setup, commit helper
- `internal/tools/builtin/git_status_test.go` — status output, empty repo, untracked files
- `internal/tools/builtin/git_diff_test.go` — staged vs unstaged, path filtering
- `internal/tools/builtin/git_log_test.go` — oneline vs medium format, max_count, path filter
- `internal/tools/builtin/git_blame_test.go` — blame output, nonexistent file

### Files to Modify

- `internal/config/config.go` — add `EnableGit bool` to `ToolsConfig`; add env var resolution
- `internal/config/defaults.go` — set `EnableGit` to `false` (requires opt-in)
- `internal/runner/runner.go` — register all git tools in `registerBuiltinTools` when enabled

### Key Decisions

- Git commands use `os/exec` to call the system `git` binary, matching the pattern of `command_exec`. No git library dependency.
- Shared `gitHelper` struct holds lazy-initialized repo root and workspace root. All git tool constructors receive a pointer to the same helper instance.
- Lazy repo root discovery: on first call, run `git rev-parse --show-toplevel`. Cache the result. If the repo root differs from the workspace root, return an error (workspace confinement).
- Git not installed: checked lazily. If `git` is not on PATH, all git tools return `ToolResult{Error: "git is not installed or not in PATH"}`.
- Output truncation: use `SandboxConfig.MaxOutputBytes` (passed as constructor param), matching `command_exec` pattern.
- All read-only git tools: `Safe: true`, `SideEffect: false`, timeout 10s.

### Acceptance Criteria

- `git_status` returns porcelain v1 output.
- `git_diff` returns unstaged diff by default, staged diff with `staged: true`.
- `git_diff` with `path` limits diff to that file.
- `git_log` returns `--oneline` by default; `oneline: false` uses medium format.
- `git_log` respects `max_count` (default 10, max 50).
- `git_blame` returns per-line attribution.
- All tools return error if workspace is not a git repo.
- All tools return error if repo root != workspace root (confinement).
- All tools truncate output at `MaxOutputBytes`.
- Path arguments validated via `workspace.ResolvePath()`.
- `git` not on PATH produces a clear error, not a crash.
- All existing tests pass.

---

## Phase 4: Git Write Tools (`git_add`, `git_commit`, `git_branch`, `git_checkout`)

### Goals

Add mutating git tools that let the agent stage changes, create commits, and manage branches. These are unsafe tools requiring explicit opt-in.

### Files to Create

- `internal/tools/builtin/git_add.go` — `NewGitAdd` constructor and schema
- `internal/tools/builtin/git_commit.go` — `NewGitCommit` constructor and schema
- `internal/tools/builtin/git_branch.go` — `NewGitBranch` constructor and schema
- `internal/tools/builtin/git_checkout.go` — `NewGitCheckout` constructor and schema
- `internal/tools/builtin/git_add_test.go` — stage files, stage all, path validation
- `internal/tools/builtin/git_commit_test.go` — commit with message, nothing staged error
- `internal/tools/builtin/git_branch_test.go` — list, create, create+checkout, invalid name
- `internal/tools/builtin/git_checkout_test.go` — switch branch, restore file, mutual exclusivity

### Files to Modify

- `internal/runner/runner.go` — register git write tools (reuse same `EnableGit` config flag)

### Key Decisions

- All write tools share the same `gitHelper` instance created in Phase 3.
- Write tools: `Safe: false`, `SideEffect: true`, timeout 20s.
- `git_commit` does not support `--amend`, `--no-verify`, or `--allow-empty`. Only `-m <message>`.
- `git_branch` validates branch names: reject spaces, `..`, `~`, `^`, `:`, `\`, control characters, leading `-`. Use a simple regex check.
- `git_checkout` uses `oneOf` schema to enforce exactly one of `branch` or `path`.
- `git_checkout -- <path>` is destructive (discards uncommitted changes). This is intentional — the agent needs to be able to restore files. Safety is handled at the policy layer (the tool is `Safe: false`) and the config layer (`enable_git` defaults to `false`). The agent's planner prompt already warns about destructive tool calls.
- No `git_push` tool. Intentionally excluded for safety.
- All write tools registered under the same `EnableGit` config flag as read tools. No separate config for read vs write git — if you trust the agent with git, you trust it with both.

### Acceptance Criteria

- `git_add` stages specified files; verified by subsequent `git_status`.
- `git_add` with `["."]` stages all changes.
- `git_add` validates paths via `workspace.ResolvePath()`.
- `git_commit` creates a commit with the given message; verified by `git_log`.
- `git_commit` returns error when nothing is staged.
- `git_branch` lists branches when `name` is omitted.
- `git_branch` creates a branch; verified by `git branch --list`.
- `git_branch` with `checkout: true` creates and switches; verified by `git rev-parse --abbrev-ref HEAD`.
- `git_branch` rejects invalid names (spaces, `..`, leading `-`).
- `git_checkout` switches branches; verified by HEAD.
- `git_checkout` with `path` restores file to committed state.
- `git_checkout` returns error when both `branch` and `path` are set (schema enforcement).
- All write tools return error if workspace is not a git repo.
- All write tools return error if repo root != workspace root.
- All existing tests pass.

---

## Phase 5: Integration, Documentation, and E2E Tests

### Goals

Update documentation, example configs, and add e2e tests that exercise the new tools with real LLM providers.

### Files to Modify

- `README.md` — add new tools to the built-in tools table, update architecture diagram, add config fields to example
- `CLAUDE.md` — add new tool descriptions to architecture section
- `examples/configs/code-assistant.yaml` — enable `workspace_find`, `workspace_grep`, `workspace_edit`, and `git` tools
- `examples/README.md` — add new task examples for search, edit, and git workflows

### Files to Create

- `internal/runner/e2e_tools_test.go` — e2e tests (gated behind API keys) that exercise search + edit + git tools in a real agent run:
  - Test: agent uses `workspace_grep` to find a pattern, then `workspace_edit` to change it
  - Test: agent uses `workspace_find` to list files, reads one, and summarizes
  - Test: agent uses `git_status` and `git_log` to describe repo state

### Acceptance Criteria

- `matter tools` output includes all 10 new tools with correct flags.
- `code-assistant.yaml` config enables all new tools.
- E2E tests pass with at least one real provider.
- README documents all new tools.
- All existing tests pass.
- `golangci-lint run ./...` passes.
- `go test ./...` passes.

---

## Dependency Order

```
Phase 1 (search tools)
  → Phase 2 (edit tool) — independent, can be parallel with Phase 1
  → Phase 3 (git read tools)
    → Phase 4 (git write tools) — depends on Phase 3 (shared gitHelper)
      → Phase 5 (integration + e2e) — depends on all prior phases
```

Phases 1 and 2 are independent and can be implemented in parallel. Phase 4 depends on Phase 3 for the shared git infrastructure.

## Risks

1. **`doublestar` dependency**: Small, well-maintained library. Adds ~200 lines of compiled code. Acceptable tradeoff vs reimplementing glob `**` support.
2. **Git binary dependency**: Git tools require `git` on PATH. Not all environments have git. Mitigated by lazy detection with a clear error message.
3. **Ambiguous edit matches**: Agents may provide `old_text` that appears multiple times. The ambiguity error with occurrence count guides the agent to provide more context. This is the same approach used by Claude Code and Codex.
4. **Large repo performance**: `workspace_find` and `workspace_grep` walk the filesystem. In very large repos (>100K files), this could be slow. Mitigated by `max_results` truncation, directory skip list, and `.gitignore` support (if a `.gitignore` exists at workspace root, its patterns are respected during walks). A future optimization could add `.matterignore` support.
5. **Git state mutations**: `git_checkout -- <path>` discards changes. `git_commit` creates permanent history. These are intentional capabilities gated behind `enable_git: true`.
