package git

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	input := "" +
		"worktree /repo\n" +
		"HEAD 123\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo-worktrees/pr-15\n" +
		"HEAD abc\n" +
		"branch refs/heads/pr/15/feature\n"

	worktrees := parseWorktreeList(input)
	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}
	if worktrees[0].Path != "/repo" || worktrees[0].Branch != "refs/heads/main" {
		t.Fatalf("unexpected first worktree: %+v", worktrees[0])
	}
	if worktrees[1].Path != "/repo-worktrees/pr-15" || worktrees[1].Branch != "refs/heads/pr/15/feature" {
		t.Fatalf("unexpected second worktree: %+v", worktrees[1])
	}
}

func TestBranchMatches(t *testing.T) {
	cases := []struct {
		ref    string
		branch string
		match  bool
	}{
		{"refs/heads/main", "main", true},
		{"main", "refs/heads/main", true},
		{"refs/heads/pr/1/branch", "pr/1/branch", true},
		{"refs/heads/other", "main", false},
	}

	for _, tc := range cases {
		if branchMatches(tc.ref, tc.branch) != tc.match {
			t.Fatalf("branchMatches(%q, %q) expected %v", tc.ref, tc.branch, tc.match)
		}
	}
}

func TestHasRemote(t *testing.T) {
	fakeRunner := &fakeRunner{
		output: "origin\nfork\nprt-fork\n",
	}
	client := &Client{runner: fakeRunner}

	has, err := client.HasRemote(nil, "/repo", "origin")
	if err != nil {
		t.Fatalf("HasRemote: %v", err)
	}
	if !has {
		t.Fatalf("expected HasRemote to return true for origin")
	}

	has, err = client.HasRemote(nil, "/repo", "prt-fork")
	if err != nil {
		t.Fatalf("HasRemote: %v", err)
	}
	if !has {
		t.Fatalf("expected HasRemote to return true for prt-fork")
	}

	has, err = client.HasRemote(nil, "/repo", "nonexistent")
	if err != nil {
		t.Fatalf("HasRemote: %v", err)
	}
	if has {
		t.Fatalf("expected HasRemote to return false for nonexistent")
	}
}

func TestRemoteURL(t *testing.T) {
	fakeRunner := &fakeRunner{
		output: "https://github.com/octo/repo.git",
	}
	client := &Client{runner: fakeRunner}

	url, err := client.RemoteURL(nil, "/repo", "origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if url != "https://github.com/octo/repo.git" {
		t.Fatalf("expected URL https://github.com/octo/repo.git, got %s", url)
	}
}

func TestWorktreeAddBranchReturnsErrBranchExists(t *testing.T) {
	runner := &fakeRunner{
		output: "fatal: a branch named 'feature' already exists",
		err:    fmt.Errorf("exit status 128"),
	}
	client := &Client{runner: runner}

	err := client.WorktreeAddBranch(context.Background(), "/repo", "/repo-wt", "feature", "origin/feature", false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("expected ErrBranchExists, got: %v", err)
	}
}

func TestWorktreeAddBranchGenericError(t *testing.T) {
	runner := &fakeRunner{
		output: "fatal: something else went wrong",
		err:    fmt.Errorf("exit status 128"),
	}
	client := &Client{runner: runner}

	err := client.WorktreeAddBranch(context.Background(), "/repo", "/repo-wt", "feature", "origin/feature", false)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrBranchExists) {
		t.Fatalf("expected generic error, not ErrBranchExists")
	}
}

type fakeRunner struct {
	output string
	err    error
}

func (r *fakeRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	return r.output, r.err
}
