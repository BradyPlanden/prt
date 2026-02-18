package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BradyPlanden/prt/internal/config"
	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/github"
)

type fakeGit struct {
	repos                 map[string]*fakeRepo
	fetches               []fetchCall
	branchFetches         []branchFetchCall
	submoduleUpdates      []string
	adds                  []addCall
	upstreams             []upstreamCall
	configs               []configCall
	branchAdds            []branchAddCall
	fetchErr              error
	branchAddFirstCallErr error
	branchAddCallCount    int
	fetchBranchErr        error
	submoduleUpdateErr    error
}

type fakeRepo struct {
	origin    string
	remotes   map[string]string
	worktrees map[string]string
}

type fetchCall struct {
	repoDir string
	remote  string
	refspec string
}

type branchFetchCall struct {
	repoDir string
	remote  string
	branch  string
}

type addCall struct {
	repoDir string
	path    string
	branch  string
}

type upstreamCall struct {
	repoDir  string
	branch   string
	upstream string
}

type configCall struct {
	repoDir string
	key     string
	value   string
}

type branchAddCall struct {
	repoDir    string
	path       string
	branch     string
	startPoint string
}

func newFakeGit() *fakeGit {
	return &fakeGit{
		repos:            make(map[string]*fakeRepo),
		fetches:          []fetchCall{},
		branchFetches:    []branchFetchCall{},
		submoduleUpdates: []string{},
		adds:             []addCall{},
		upstreams:        []upstreamCall{},
		configs:          []configCall{},
		branchAdds:       []branchAddCall{},
	}
}

func (f *fakeGit) IsGitRepo(_ context.Context, repoDir string) (bool, error) {
	_, ok := f.repos[repoDir]
	return ok, nil
}

func (f *fakeGit) Clone(_ context.Context, url string, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	f.repos[dest] = &fakeRepo{
		origin:    url,
		remotes:   map[string]string{"origin": url},
		worktrees: map[string]string{},
	}
	return nil
}

func (f *fakeGit) CloneBare(ctx context.Context, url string, dest string, _ int) error {
	return f.Clone(ctx, url, dest)
}

func (f *fakeGit) Fetch(_ context.Context, repoDir string, remote string, refspec string) error {
	f.fetches = append(f.fetches, fetchCall{repoDir: repoDir, remote: remote, refspec: refspec})
	return f.fetchErr
}

func (f *fakeGit) FetchBranch(_ context.Context, repoDir string, remote string, branch string) error {
	f.branchFetches = append(f.branchFetches, branchFetchCall{repoDir: repoDir, remote: remote, branch: branch})
	return f.fetchBranchErr
}

func (f *fakeGit) SubmoduleUpdate(_ context.Context, repoDir string) error {
	f.submoduleUpdates = append(f.submoduleUpdates, repoDir)
	return f.submoduleUpdateErr
}

func (f *fakeGit) WorktreeAdd(_ context.Context, repoDir string, worktreePath string, branch string) error {
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return err
	}
	f.repos[repoDir].worktrees[branch] = worktreePath
	f.adds = append(f.adds, addCall{repoDir: repoDir, path: worktreePath, branch: branch})
	return nil
}

func (f *fakeGit) WorktreeRemove(_ context.Context, repoDir string, worktreePath string, _ bool) error {
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

func (f *fakeGit) WorktreeList(_ context.Context, repoDir string) ([]git.Worktree, error) {
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

func (f *fakeGit) HasWorktreeForBranch(_ context.Context, repoDir string, branch string) (string, bool, error) {
	if repo, ok := f.repos[repoDir]; ok {
		if path, ok := repo.worktrees[branch]; ok {
			return path, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeGit) OriginURL(_ context.Context, repoDir string) (string, error) {
	repo, ok := f.repos[repoDir]
	if !ok {
		return "", nil
	}
	return repo.origin, nil
}

func (f *fakeGit) AddRemote(_ context.Context, repoDir string, name string, url string) error {
	repo, ok := f.repos[repoDir]
	if !ok {
		return nil
	}
	if repo.remotes == nil {
		repo.remotes = make(map[string]string)
	}
	repo.remotes[name] = url
	return nil
}

func (f *fakeGit) HasRemote(_ context.Context, repoDir string, name string) (bool, error) {
	repo, ok := f.repos[repoDir]
	if !ok {
		return false, nil
	}
	_, exists := repo.remotes[name]
	return exists, nil
}

func (f *fakeGit) SetUpstream(_ context.Context, repoDir string, branch string, upstream string) error {
	f.upstreams = append(f.upstreams, upstreamCall{repoDir: repoDir, branch: branch, upstream: upstream})
	return nil
}

func (f *fakeGit) ConfigSet(_ context.Context, repoDir string, key string, value string) error {
	f.configs = append(f.configs, configCall{repoDir: repoDir, key: key, value: value})
	return nil
}

func (f *fakeGit) ConfigSetWorktree(_ context.Context, repoDir string, key string, value string) error {
	f.configs = append(f.configs, configCall{repoDir: repoDir, key: "--worktree:" + key, value: value})
	return nil
}

func (f *fakeGit) WorktreeAddBranch(_ context.Context, repoDir string, worktreePath string, branch string, startPoint string, _ bool) error {
	if f.branchAddFirstCallErr != nil && f.branchAddCallCount == 0 {
		f.branchAddCallCount++
		return f.branchAddFirstCallErr
	}
	f.branchAddCallCount++
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return err
	}
	f.repos[repoDir].worktrees[branch] = worktreePath
	f.branchAdds = append(f.branchAdds, branchAddCall{repoDir: repoDir, path: worktreePath, branch: branch, startPoint: startPoint})
	return nil
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
	if len(fake.fetches) != 1 {
		t.Fatalf("expected one fetch")
	}
	if fake.fetches[0].refspec != "+refs/heads/feature:refs/remotes/origin/feature" {
		t.Fatalf("expected refspec +refs/heads/feature:refs/remotes/origin/feature, got %s", fake.fetches[0].refspec)
	}
	if len(fake.branchAdds) != 1 {
		t.Fatalf("expected WorktreeAddBranch to be called")
	}
	if fake.branchAdds[0].startPoint != "origin/feature" {
		t.Fatalf("expected startPoint origin/feature, got %s", fake.branchAdds[0].startPoint)
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].repoDir != expectedWorktree {
		t.Fatalf("expected upstream configured in %s, got %s", expectedWorktree, fake.upstreams[0].repoDir)
	}
	if fake.upstreams[0].branch != "feature" {
		t.Fatalf("expected upstream branch feature, got %s", fake.upstreams[0].branch)
	}
	if fake.upstreams[0].upstream != "origin/feature" {
		t.Fatalf("expected upstream origin/feature, got %s", fake.upstreams[0].upstream)
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
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{"feature": worktreePath},
	}

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
	if len(fake.branchAdds) != 0 {
		t.Fatalf("expected no worktree add")
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].upstream != "origin/feature" {
		t.Fatalf("expected upstream origin/feature, got %s", fake.upstreams[0].upstream)
	}
}

type testLogger struct {
	messages []string
}

func (l *testLogger) Printf(format string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(format, args...))
}

func TestResolveReusesWorktreeWhenFetchFails(t *testing.T) {
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
	fake.fetchErr = errors.New("network unreachable")
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{"feature": worktreePath},
	}

	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	logger := &testLogger{}
	resolver := NewResolver(fake, ResolverOptions{Logger: logger})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("expected reuse to succeed despite fetch failure, got: %v", err)
	}
	if !result.Reused {
		t.Fatalf("expected reuse")
	}
	if result.Path != worktreePath {
		t.Fatalf("expected %s, got %s", worktreePath, result.Path)
	}
	if len(logger.messages) == 0 {
		t.Fatalf("expected a warning to be logged")
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected Warnings to be populated in Result")
	}
}

func TestResolveReusesWorktreeWhenFetchFailsNoLogger(t *testing.T) {
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
	fake.fetchErr = errors.New("network unreachable")
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{"feature": worktreePath},
	}

	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("expected reuse to succeed despite fetch failure, got: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected Warnings to be populated even without logger")
	}
}

func TestResolvePersistentRecoversStaleBranch(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	fake := newFakeGit()
	fake.branchAddFirstCallErr = fmt.Errorf("git worktree add -b failed: %w", git.ErrBranchExists)
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{},
	}

	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("expected recovery from stale branch, got: %v", err)
	}

	expectedWorktree := repoDir + "-worktrees/pr-15-feature"
	if result.Path != expectedWorktree {
		t.Fatalf("expected worktree %s, got %s", expectedWorktree, result.Path)
	}
	if len(fake.branchAdds) != 1 {
		t.Fatalf("expected one successful WorktreeAddBranch after retry")
	}
}

func TestResolvePersistentDoesNotRetryNonBranchError(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	fake := newFakeGit()
	fake.branchAddFirstCallErr = errors.New("disk full")
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{},
	}

	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	resolver := NewResolver(fake, ResolverOptions{})
	_, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err == nil {
		t.Fatalf("expected error for non-branch-exists failure")
	}
	if fake.branchAddCallCount != 1 {
		t.Fatalf("expected exactly one WorktreeAddBranch call (no retry), got %d", fake.branchAddCallCount)
	}
}

func TestResolveForkUsesNamespacedBranch(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "fork", "repo", "fix/bug", 21)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(fake.fetches) != 1 {
		t.Fatalf("expected one fetch")
	}
	fetch := fake.fetches[0]
	if fetch.remote != "prt/fork/repo" {
		t.Fatalf("unexpected fetch remote: %s", fetch.remote)
	}
	if fetch.refspec != "+refs/heads/fix/bug:refs/remotes/prt/fork/repo/fix/bug" {
		t.Fatalf("unexpected refspec: %s", fetch.refspec)
	}
	if len(fake.branchAdds) != 1 {
		t.Fatalf("expected WorktreeAddBranch to be called")
	}
	if fake.branchAdds[0].startPoint != "prt/fork/repo/fix/bug" {
		t.Fatalf("expected startPoint prt/fork/repo/fix/bug, got %s", fake.branchAdds[0].startPoint)
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].repoDir != result.Path {
		t.Fatalf("expected upstream configured in %s, got %s", result.Path, fake.upstreams[0].repoDir)
	}
	if fake.upstreams[0].branch != "pr/21/fix/bug" {
		t.Fatalf("expected upstream branch pr/21/fix/bug, got %s", fake.upstreams[0].branch)
	}
	if fake.upstreams[0].upstream != "prt/fork/repo/fix/bug" {
		t.Fatalf("expected upstream prt/fork/repo/fix/bug, got %s", fake.upstreams[0].upstream)
	}
}

func TestCleanTempRemovesOldWorktree(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "octo-repo.git")
	worktreeOld := filepath.Join(tempDir, "octo-repo-pr-1-old")
	worktreeNew := filepath.Join(tempDir, "octo-repo-pr-2-new")

	for _, dir := range []string{bareDir, worktreeOld, worktreeNew} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
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
	results, err := resolver.CleanTemp(context.Background(), tempDir, 24*time.Hour, false, false)
	if err != nil {
		t.Fatalf("clean temp: %v", err)
	}
	if len(results) != 1 || results[0].Path != worktreeOld {
		t.Fatalf("expected only old worktree in results, got %v", results)
	}
	if _, err := os.Stat(worktreeOld); !os.IsNotExist(err) {
		t.Fatalf("expected old worktree to be removed")
	}
	if _, err := os.Stat(worktreeNew); err != nil {
		t.Fatalf("expected new worktree to remain: %v", err)
	}
	// Bare repo should remain because worktreeNew still exists.
	if _, err := os.Stat(bareDir); err != nil {
		t.Fatalf("expected bare repo to remain: %v", err)
	}
}

func TestCleanTempRemovesBareRepoWhenAllWorktreesGone(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "octo-repo.git")
	worktreeOld := filepath.Join(tempDir, "octo-repo-pr-1-old")

	for _, dir := range []string{bareDir, worktreeOld} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(worktreeOld, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	fake := newFakeGit()
	fake.repos[bareDir] = &fakeRepo{origin: "https://github.com/octo/repo.git", worktrees: map[string]string{
		"pr/1/old": worktreeOld,
	}}

	resolver := NewResolver(fake, ResolverOptions{})
	_, err := resolver.CleanTemp(context.Background(), tempDir, 24*time.Hour, false, false)
	if err != nil {
		t.Fatalf("clean temp: %v", err)
	}
	// All worktrees removed — bare repo should be cleaned up too.
	if _, err := os.Stat(bareDir); !os.IsNotExist(err) {
		t.Fatalf("expected bare repo to be removed when no worktrees remain")
	}
}

func TestCleanTempAll(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "octo-repo.git")
	worktreeRecent := filepath.Join(tempDir, "octo-repo-pr-1-recent")
	worktreeOld := filepath.Join(tempDir, "octo-repo-pr-2-old")

	for _, dir := range []string{bareDir, worktreeRecent, worktreeOld} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(worktreeOld, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	fake := newFakeGit()
	fake.repos[bareDir] = &fakeRepo{origin: "https://github.com/octo/repo.git", worktrees: map[string]string{
		"pr/1/recent": worktreeRecent,
		"pr/2/old":    worktreeOld,
	}}

	resolver := NewResolver(fake, ResolverOptions{})
	results, err := resolver.CleanTemp(context.Background(), tempDir, 24*time.Hour, true, false)
	if err != nil {
		t.Fatalf("clean temp: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both worktrees removed, got %d results", len(results))
	}
	for _, r := range results {
		if _, err := os.Stat(r.Path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed", r.Path)
		}
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

func TestEnsureRepoAddsOriginIfMissing(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	fake := newFakeGit()
	fake.repos[repoDir] = &fakeRepo{
		origin:    "",
		remotes:   map[string]string{},
		worktrees: map[string]string{},
	}

	err := ensureRepo(context.Background(), fake, repoDir, "https://github.com/octo/repo.git")
	if err != nil {
		t.Fatalf("ensureRepo: %v", err)
	}

	url, exists := fake.repos[repoDir].remotes["origin"]
	if !exists {
		t.Fatalf("expected origin remote to be added")
	}
	if url != "https://github.com/octo/repo.git" {
		t.Fatalf("expected origin URL https://github.com/octo/repo.git, got %s", url)
	}
}

func TestEnsureRemotePreservesExistingURL(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	fake := newFakeGit()
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/wrong/repo.git",
		remotes:   map[string]string{"prt/fork/repo-repo": "git@github.com:fork/repo.git"},
		worktrees: map[string]string{},
	}

	err := ensureRemote(context.Background(), fake, repoDir, "prt/fork/repo-repo", "https://github.com/fork/repo.git")
	if err != nil {
		t.Fatalf("ensureRemote: %v", err)
	}

	url := fake.repos[repoDir].remotes["prt/fork/repo-repo"]
	if url != "git@github.com:fork/repo.git" {
		t.Fatalf("expected existing SSH URL to be preserved, got %s", url)
	}
}

func TestResolveRepoDir_NoOrigin_UsesAlternate(t *testing.T) {
	projectsDir := t.TempDir()
	repoDir := filepath.Join(projectsDir, "repo")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	fake := newFakeGit()
	fake.repos[repoDir] = &fakeRepo{
		origin:    "",
		remotes:   map[string]string{},
		worktrees: map[string]string{},
	}

	repo := github.Repository{Owner: "octo", Name: "repo", CloneURL: "https://github.com/octo/repo.git"}
	resolved, err := resolveRepoDir(context.Background(), fake, projectsDir, repo, nil)
	if err != nil {
		t.Fatalf("resolveRepoDir: %v", err)
	}

	expected := filepath.Join(projectsDir, "octo-repo")
	if resolved != expected {
		t.Fatalf("expected alternate path %s, got %s", expected, resolved)
	}
}

func TestResolveTempSameRepo(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{ProjectsDir: t.TempDir(), TempDir: tempDir, TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	expectedBare := filepath.Join(tempDir, "octo-repo.git")
	expectedWorktree := filepath.Join(tempDir, "octo-repo-pr-15-feature")

	if result.Path != expectedWorktree {
		t.Fatalf("expected worktree %s, got %s", expectedWorktree, result.Path)
	}
	if result.RepoDir != expectedBare {
		t.Fatalf("expected bare repo %s, got %s", expectedBare, result.RepoDir)
	}
	if len(fake.branchAdds) != 1 {
		t.Fatalf("expected WorktreeAddBranch to be called")
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].repoDir != expectedWorktree {
		t.Fatalf("expected upstream configured in %s, got %s", expectedWorktree, fake.upstreams[0].repoDir)
	}
	if fake.upstreams[0].branch != "feature" {
		t.Fatalf("expected upstream branch feature, got %s", fake.upstreams[0].branch)
	}
	if fake.upstreams[0].upstream != "origin/feature" {
		t.Fatalf("expected upstream origin/feature, got %s", fake.upstreams[0].upstream)
	}
}

func TestResolveTempCrossRepo(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{ProjectsDir: t.TempDir(), TempDir: tempDir, TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "fork", "repo", "fix/bug", 21)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(fake.fetches) != 1 {
		t.Fatalf("expected one fetch")
	}
	if fake.fetches[0].remote != "prt/fork/repo" {
		t.Fatalf("unexpected fetch remote: %s", fake.fetches[0].remote)
	}
	if len(fake.branchAdds) != 1 {
		t.Fatalf("expected WorktreeAddBranch to be called")
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].repoDir != result.Path {
		t.Fatalf("expected upstream configured in %s, got %s", result.Path, fake.upstreams[0].repoDir)
	}
	if fake.upstreams[0].branch != "pr/21/fix/bug" {
		t.Fatalf("expected upstream branch pr/21/fix/bug, got %s", fake.upstreams[0].branch)
	}
	if fake.upstreams[0].upstream != "prt/fork/repo/fix/bug" {
		t.Fatalf("expected upstream prt/fork/repo/fix/bug, got %s", fake.upstreams[0].upstream)
	}

	foundWorktreeConfig := false
	foundPushDefault := false
	for _, cfg := range fake.configs {
		if cfg.key == "extensions.worktreeConfig" && cfg.value == "true" {
			foundWorktreeConfig = true
		}
		if cfg.key == "--worktree:push.default" && cfg.value == "upstream" && cfg.repoDir == result.Path {
			foundPushDefault = true
		}
	}

	if !foundWorktreeConfig {
		t.Fatalf("expected extensions.worktreeConfig to be set")
	}
	if !foundPushDefault {
		t.Fatalf("expected per-worktree push.default to be set")
	}
}

func TestResolveTempReusesExistingWorktree(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "octo-repo.git")
	worktreePath := filepath.Join(tempDir, "octo-repo-pr-15-feature")

	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	fake := newFakeGit()
	fake.repos[bareDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{"feature": worktreePath},
	}

	cfg := config.Config{ProjectsDir: t.TempDir(), TempDir: tempDir, TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.Reused {
		t.Fatalf("expected reuse")
	}
	if result.Path != worktreePath {
		t.Fatalf("expected %s, got %s", worktreePath, result.Path)
	}
	if len(fake.branchAdds) != 0 {
		t.Fatalf("expected no worktree add")
	}
	if len(fake.upstreams) != 1 {
		t.Fatalf("expected SetUpstream to be called")
	}
	if fake.upstreams[0].branch != "feature" {
		t.Fatalf("expected upstream branch feature, got %s", fake.upstreams[0].branch)
	}
	if fake.upstreams[0].upstream != "origin/feature" {
		t.Fatalf("expected upstream origin/feature, got %s", fake.upstreams[0].upstream)
	}
}

func TestCrossRepoPushConfig(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "fork", "repo", "fix/bug", 21)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	foundWorktreeConfig := false
	foundPushDefault := false
	for _, cfg := range fake.configs {
		if cfg.key == "extensions.worktreeConfig" && cfg.value == "true" {
			foundWorktreeConfig = true
		}
		if cfg.key == "--worktree:push.default" && cfg.value == "upstream" && cfg.repoDir == result.Path {
			foundPushDefault = true
		}
	}

	if !foundWorktreeConfig {
		t.Fatalf("expected extensions.worktreeConfig to be set")
	}
	if !foundPushDefault {
		t.Fatalf("expected per-worktree push.default to be set")
	}
}

func makePR(baseOwner, baseRepo, headOwner, headRepo, headRef string, number int) github.PRMetadata {
	return github.PRMetadata{
		Number:  number,
		HeadRef: headRef,
		BaseRef: "main",
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

func TestResolveNewWorktreeFetchesBaseBranch(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	fake := newFakeGit()
	resolver := NewResolver(fake, ResolverOptions{})

	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify base branch fetch was attempted.
	if len(fake.branchFetches) != 1 {
		t.Fatalf("expected one base branch fetch, got %d", len(fake.branchFetches))
	}
	if fake.branchFetches[0].branch != "main" {
		t.Fatalf("expected base branch main, got %s", fake.branchFetches[0].branch)
	}
	if fake.branchFetches[0].remote != "origin" {
		t.Fatalf("expected remote origin, got %s", fake.branchFetches[0].remote)
	}

	// Verify submodule update was attempted in the worktree.
	if len(fake.submoduleUpdates) != 1 {
		t.Fatalf("expected one submodule update, got %d", len(fake.submoduleUpdates))
	}
	if fake.submoduleUpdates[0] != result.Path {
		t.Fatalf("expected submodule update in %s, got %s", result.Path, fake.submoduleUpdates[0])
	}
}

func TestResolveReusedWorktreeUpdatesSubmodules(t *testing.T) {
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
	fake.repos[repoDir] = &fakeRepo{
		origin:    "https://github.com/octo/repo.git",
		remotes:   map[string]string{"origin": "https://github.com/octo/repo.git"},
		worktrees: map[string]string{"feature": worktreePath},
	}

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

	// Verify submodule update was still attempted.
	if len(fake.submoduleUpdates) != 1 {
		t.Fatalf("expected one submodule update, got %d", len(fake.submoduleUpdates))
	}
	if fake.submoduleUpdates[0] != worktreePath {
		t.Fatalf("expected submodule update in %s, got %s", worktreePath, fake.submoduleUpdates[0])
	}
}

func TestResolveBaseBranchFetchFailureProducesWarning(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	fake := newFakeGit()
	fake.fetchBranchErr = errors.New("network unreachable")

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("expected resolution to succeed despite fetch failure, got: %v", err)
	}

	// Verify a warning was produced.
	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning for failed base branch fetch")
	}
	foundBaseBranchWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "base branch") {
			foundBaseBranchWarning = true
			break
		}
	}
	if !foundBaseBranchWarning {
		t.Fatalf("expected a warning about base branch fetch failure, got: %v", result.Warnings)
	}
}

func TestResolveSubmoduleUpdateFailureProducesWarning(t *testing.T) {
	projectsDir := t.TempDir()
	cfg := config.Config{ProjectsDir: projectsDir, TempDir: t.TempDir(), TempTTL: 24 * time.Hour}
	pr := makePR("octo", "repo", "octo", "repo", "feature", 15)

	fake := newFakeGit()
	fake.submoduleUpdateErr = errors.New("submodule init failed")

	resolver := NewResolver(fake, ResolverOptions{})
	result, err := resolver.Resolve(context.Background(), cfg, pr, Options{Temp: false})
	if err != nil {
		t.Fatalf("expected resolution to succeed despite submodule failure, got: %v", err)
	}

	// Verify a warning was produced.
	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning for failed submodule init")
	}
	foundSubmoduleWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "submodule") {
			foundSubmoduleWarning = true
			break
		}
	}
	if !foundSubmoduleWarning {
		t.Fatalf("expected a warning about submodule init failure, got: %v", result.Warnings)
	}
}
