package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (string, error)
}

type ExecRunner struct {
	Verbose bool
	Logger  Logger
}

type Logger interface {
	Printf(format string, args ...any)
}

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

type Client struct {
	runner Runner
}

type ClientOptions struct {
	Verbose bool
	Logger  Logger
	Runner  Runner
}

func NewClient(opts ClientOptions) *Client {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{Verbose: opts.Verbose, Logger: opts.Logger}
	}
	return &Client{runner: runner}
}

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

func (c *Client) Clone(ctx context.Context, url string, dest string) error {
	_, err := c.runner.Run(ctx, "", "git", "clone", url, dest)
	if err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

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

func (c *Client) Fetch(ctx context.Context, repoDir string, remote string, refspec string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "fetch", remote, refspec)
	if err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	return nil
}

func (c *Client) WorktreeAdd(ctx context.Context, repoDir string, worktreePath string, branch string) error {
	_, err := c.runner.Run(ctx, repoDir, "git", "worktree", "add", worktreePath, branch)
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

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

func (c *Client) WorktreeList(ctx context.Context, repoDir string) ([]Worktree, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}
	return parseWorktreeList(output), nil
}

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

func (c *Client) OriginURL(ctx context.Context, repoDir string) (string, error) {
	output, err := c.runner.Run(ctx, repoDir, "git", "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("git config --get remote.origin.url failed: %w", err)
	}
	return strings.TrimSpace(output), nil
}

type Worktree struct {
	Path   string
	Branch string
}

func parseWorktreeList(output string) []Worktree {
	var worktrees []Worktree
	var current *Worktree

	lines := strings.Split(output, "\n")
	for _, line := range lines {
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
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/") == branch
	}
	if strings.HasPrefix(branch, "refs/heads/") {
		return strings.TrimPrefix(branch, "refs/heads/") == ref
	}
	return false
}
