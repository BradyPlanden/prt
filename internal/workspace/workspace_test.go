package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BradyPlanden/prt/internal/config"
	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/github"
)

type fakeGit struct {
	repos   map[string]*fakeRepo
	fetches []fetchCall
	adds    []addCall
}

type fakeRepo struct {
	origin    string
	worktrees map[string]string
}

type fetchCall struct {
	repoDir string
	remote  string
	refspec string
}

type addCall struct {
	repoDir string
	path    string
	branch  string
}

func newFakeGit() *fakeGit {
	return &fakeGit{repos: make(map[string]*fakeRepo)}
}

func (f *fakeGit) IsGitRepo(ctx context.Context, repoDir string) (bool, error) {
	_, ok := f.repos[repoDir]
	return ok, nil
}

func (f *fakeGit) Clone(ctx context.Context, url string, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	f.repos[dest] = &fakeRepo{origin: url, worktrees: map[string]string{}}
	return nil
}

func (f *fakeGit) CloneBare(ctx context.Context, url string, dest string, depth int) error {
	return f.Clone(ctx, url, dest)
}

func (f *fakeGit) Fetch(ctx context.Context, repoDir string, remote string, refspec string) error {
	f.fetches = append(f.fetches, fetchCall{repoDir: repoDir, remote: remote, refspec: refspec})
	return nil
}

func (f *fakeGit) WorktreeAdd(ctx context.Context, repoDir string, worktreePath string, branch string) error {
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return err
	}
	f.repos[repoDir].worktrees[branch] = worktreePath
	f.adds = append(f.adds, addCall{repoDir: repoDir, path: worktreePath, branch: branch})
	return nil
}

func (f *fakeGit) WorktreeRemove(ctx context.Context, repoDir string, worktreePath string, force bool) error {
	if err := os.RemoveAll(worktreePath); err != nil {
		return err
	}
	for branch, path := range f.repos[repoDir].worktrees {
		if path == worktreePath {
			delete(f.repos[repoDir].worktrees, branch)
			break
		}
	}
	return nil
}

func (f *fakeGit) WorktreeList(ctx context.Context, repoDir string) ([]git.Worktree, error) {
	repo, ok := f.repos[repoDir]
	if !ok {
		return nil, nil
	}
	var worktrees []git.Worktree
	for branch, path := range repo.worktrees {
		worktrees = append(worktrees, git.Worktree{Path: path, Branch: "refs/heads/" + branch})
	}
	return worktrees, nil
}

func (f *fakeGit) HasWorktreeForBranch(ctx context.Context, repoDir string, branch string) (string, bool, error) {
	if repo, ok := f.repos[repoDir]; ok {
		if path, ok := repo.worktrees[branch]; ok {
			return path, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeGit) OriginURL(ctx context.Context, repoDir string) (string, error) {
	repo, ok := f.repos[repoDir]
	if !ok {
		return "", nil
	}
	return repo.origin, nil
}

func TestResolvePersistentCloneAndWorktree(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	expectedRepo := filepath.Join(projectsDir, "repo")
	expectedWorktree := expectedRepo + "-worktrees/pr-15-feature"

	if result.Path != expectedWorktree {
		t.Fatalf("expected worktree %s, got %s", expectedWorktree, result.Path)
	}
	if _, ok := fake.repos[expectedRepo]; !ok {
		t.Fatalf("expected repo to be cloned")
	}
	if len(fake.adds) != 1 {
		t.Fatalf("expected worktree add to be called")
	}
}

func TestResolveReusesExistingWorktree(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")
	worktreePath := repoDir + "-worktrees/pr-15-feature"

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	fake := newFakeGit()
	fake.repos[repoDir] = &fakeRepo{origin: "https://github.com/octo/repo.git", worktrees: map[string]string{"feature": worktreePath}}

	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.Reused {
		t.Fatalf("expected reuse")
	}
	if result.Path != worktreePath {
		t.Fatalf("expected %s, got %s", worktreePath, result.Path)
	}
	if len(fake.adds) != 0 {
		t.Fatalf("expected no worktree add")
	}
}

func TestResolveForkUsesNamespacedBranch(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "fork", "repo", "fix/bug", 21)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	_, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(fake.fetches) != 1 {
		t.Fatalf("expected one fetch")
	}
	fetch := fake.fetches[0]
	if fetch.remote != "https://github.com/fork/repo.git" {
		t.Fatalf("unexpected fetch remote: %s", fetch.remote)
	}
	if fetch.refspec != "+fix/bug:pr/21/fix/bug" {
		t.Fatalf("unexpected refspec: %s", fetch.refspec)
	}
}

func TestCleanTempDryRun(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "octo-repo.git")
	worktreeOld := filepath.Join(tempDir, "octo-repo-pr-1-old")
	worktreeNew := filepath.Join(tempDir, "octo-repo-pr-2-new")

	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	if err := os.MkdirAll(worktreeOld, 0o755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := os.MkdirAll(worktreeNew, 0o755); err != nil {
		t.Fatalf("mkdir new: %v", err)
	}

	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(worktreeOld, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	fake := newFakeGit()
	fake.repos[bareDir] = &fakeRepo{origin: "https://github.com/octo/repo.git", worktrees: map[string]string{
		"pr/1/old": worktreeOld,
		"pr/2/new": worktreeNew,
	}}

	resolver := NewResolver(fake, ResolverOptions{})
	results, err := resolver.CleanTemp(context.Background(), tempDir, 24*time.Hour, false, true)
	if err != nil {
		t.Fatalf("clean temp: %v", err)
	}
	if len(results) != 1 || results[0].Path != worktreeOld {
		t.Fatalf("expected only old worktree to be listed")
	}
}

func makePR(baseOwner, baseRepo, headOwner, headRepo, headRef string, number int) github.PRMetadata {
	return github.PRMetadata{
		Number:  number,
		HeadRef: headRef,
		BaseRepo: github.Repository{
			Owner:    baseOwner,
			Name:     baseRepo,
			CloneURL: "https://github.com/" + baseOwner + "/" + baseRepo + ".git",
		},
		HeadRepo: github.Repository{
			Owner:    headOwner,
			Name:     headRepo,
			CloneURL: "https://github.com/" + headOwner + "/" + headRepo + ".git",
		},
	}
}
