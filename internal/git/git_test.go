package git

import "testing"

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
