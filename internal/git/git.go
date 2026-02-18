package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrBranchExists is returned when a branch creation fails because the
// branch already exists (e.g. stale leftover after manual worktree removal).
var ErrBranchExists = errors.New("branch already exists")

// Runner executes git commands in a working directory.
type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (string, error)
}

// ExecRunner runs commands via os/exec and optionally logs them.
type ExecRunner struct {
	Verbose bool
	Logger  Logger
}

// Logger provides lightweight structured logging hooks.
type Logger interface {
	Printf(format string, args ...any)
}

// Run executes a command and returns trimmed combined output.
func (r ExecRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	if r.Verbose && r.Logger != nil {
		r.Logger.Printf("+ %s %s", name, strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), err
	}
	return strings.TrimSpace(string(output)), nil
}

// Client wraps git command operations used by workspace resolution.
type Client struct {
	runner Runner
}

// ClientOptions configures a git client.
type ClientOptions struct {
	Verbose bool
	Logger  Logger
	Runner  Runner
}

// NewClient constructs a Client using ExecRunner when no Runner is provided.
func NewClient(opts ClientOptions) *Client {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{Verbose: opts.Verbose, Logger: opts.Logger}
	}
	return &Client{runner: runner}
}

// IsGitRepo reports whether repoDir is a valid git repository.
func (c *Client) IsGitRepo(ctx context.Context, repoDir string) (bool, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "rev-parse", "--git-dir")
	if err != nil {
		if strings.Contains(output, "not a git repository") {
			return false, nil
		}
		if errors.Is(err, exec.ErrNotFound) {
			return false, errors.New("git not found; install git to continue")
		}
		return false, fmt.Errorf("git rev-parse failed: %w", err)
	}
	return output != "", nil
}

// Clone clones a repository into dest.
func (c *Client) Clone(ctx context.Context, url string, dest string) error {
	_, err := c.runner.Run(ctx, "", "git", "clone", url, dest)
	if err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

// CloneBare clones a repository as bare into dest.
func (c *Client) CloneBare(ctx context.Context, url string, dest string, depth int) error {
	args := []string{"clone", "--bare"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	args = append(args, url, dest)
	_, err := c.runner.Run(ctx, "", "git", args...)
	if err != nil {
		return fmt.Errorf("git clone --bare failed: %w", err)
	}
	return nil
}

// Fetch fetches refspec from remote into repoDir.
func (c *Client) Fetch(ctx context.Context, repoDir string, remote string, refspec string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "fetch", remote, refspec)
	if err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	return nil
}

// FetchBranch fetches a single branch from remote into repoDir.
func (c *Client) FetchBranch(ctx context.Context, repoDir string, remote string, branch string) error {
	return c.Fetch(ctx, repoDir, remote, branch)
}

// SubmoduleUpdate initializes and updates submodules recursively in repoDir.
func (c *Client) SubmoduleUpdate(ctx context.Context, repoDir string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "submodule", "update", "--init", "--recursive")
	if err != nil {
		return fmt.Errorf("git submodule update failed: %w", err)
	}
	return nil
}

// WorktreeAdd adds a worktree for branch at worktreePath.
func (c *Client) WorktreeAdd(ctx context.Context, repoDir string, worktreePath string, branch string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "worktree", "add", worktreePath, branch)
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// WorktreeRemove removes a worktree from repoDir.
func (c *Client) WorktreeRemove(ctx context.Context, repoDir string, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := c.runner.Run(ctx, repoDir, "git", args...)
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}
	return nil
}

// WorktreeList returns parsed worktree entries for repoDir.
func (c *Client) WorktreeList(ctx context.Context, repoDir string) ([]Worktree, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}
	return parseWorktreeList(output), nil
}

// HasWorktreeForBranch reports the path of an existing worktree for branch.
func (c *Client) HasWorktreeForBranch(ctx context.Context, repoDir string, branch string) (string, bool, error) {
	worktrees, err := c.WorktreeList(ctx, repoDir)
	if err != nil {
		return "", false, err
	}
	for _, wt := range worktrees {
		if branchMatches(wt.Branch, branch) {
			return wt.Path, true, nil
		}
	}
	return "", false, nil
}

// AddRemote adds a git remote to repoDir.
func (c *Client) AddRemote(ctx context.Context, repoDir string, name string, url string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "remote", "add", name, url)
	if err != nil {
		return fmt.Errorf("git remote add failed: %w", err)
	}
	return nil
}

// HasRemote reports whether repoDir already defines remote name.
func (c *Client) HasRemote(ctx context.Context, repoDir string, name string) (bool, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "remote")
	if err != nil {
		return false, fmt.Errorf("git remote failed: %w", err)
	}
	remotes := strings.SplitSeq(output, "\n")
	for remote := range remotes {
		if strings.TrimSpace(remote) == name {
			return true, nil
		}
	}
	return false, nil
}

// RemoteURL returns the configured URL for remote name.
func (c *Client) RemoteURL(ctx context.Context, repoDir string, name string) (string, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "config", "--get", fmt.Sprintf("remote.%s.url", name))
	if err != nil {
		return "", fmt.Errorf("git config --get remote.%s.url failed: %w", name, err)
	}
	return strings.TrimSpace(output), nil
}

// SetRemoteURL updates the configured URL for remote name.
func (c *Client) SetRemoteURL(ctx context.Context, repoDir string, name string, url string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "remote", "set-url", name, url)
	if err != nil {
		return fmt.Errorf("git remote set-url failed: %w", err)
	}
	return nil
}

// SetUpstream sets branch to track upstream.
func (c *Client) SetUpstream(ctx context.Context, repoDir string, branch string, upstream string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "branch", "--set-upstream-to="+upstream, branch)
	if err != nil {
		return fmt.Errorf("git branch --set-upstream-to failed: %w", err)
	}
	return nil
}

// ConfigSet writes a git config key in repoDir.
func (c *Client) ConfigSet(ctx context.Context, repoDir string, key string, value string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "config", key, value)
	if err != nil {
		return fmt.Errorf("git config failed: %w", err)
	}
	return nil
}

// ConfigSetWorktree writes a worktree-local git config key in repoDir.
func (c *Client) ConfigSetWorktree(ctx context.Context, repoDir string, key string, value string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "config", "--worktree", key, value)
	if err != nil {
		return fmt.Errorf("git config --worktree failed: %w", err)
	}
	return nil
}

// WorktreeAddBranch adds a worktree and creates or resets branch from startPoint.
func (c *Client) WorktreeAddBranch(ctx context.Context, repoDir string, worktreePath string, branch string, startPoint string, force bool) error {
	flag := "-b"
	if force {
		flag = "-B"
	}
	output, err := c.runner.Run(ctx, repoDir, "git", "worktree", "add", flag, branch, worktreePath, startPoint)
	if err != nil {
		if !force && strings.Contains(output, "already exists") {
			return fmt.Errorf("git worktree add %s failed: %w", flag, ErrBranchExists)
		}
		return fmt.Errorf("git worktree add %s failed: %w", flag, err)
	}
	return nil
}

// OriginURL returns the URL configured for origin.
func (c *Client) OriginURL(ctx context.Context, repoDir string) (string, error) {
	return c.RemoteURL(ctx, repoDir, "origin")
}

// Worktree describes a git worktree path and branch.
type Worktree struct {
	Path   string
	Branch string
}

func parseWorktreeList(output string) []Worktree {
	var worktrees []Worktree
	var current *Worktree

	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				worktrees = append(worktrees, *current)
			}
			current = &Worktree{Path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
			continue
		}
		if strings.HasPrefix(line, "branch ") && current != nil {
			current.Branch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		}
	}

	if current != nil {
		worktrees = append(worktrees, *current)
	}

	return worktrees
}

func branchMatches(ref string, branch string) bool {
	if ref == branch {
		return true
	}
	if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return after == branch
	}
	if after, ok := strings.CutPrefix(branch, "refs/heads/"); ok {
		return after == ref
	}
	return false
}
