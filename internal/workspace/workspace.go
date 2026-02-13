package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BradyPlanden/prt/internal/config"
	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/github"
)

type Options struct {
	Temp bool
}

type Result struct {
	Path     string
	RepoDir  string
	Reused   bool
	Warnings []string
}

type CleanResult struct {
	Path string
}

type Resolver struct {
	git    GitClient
	logger Logger
}

type Logger interface {
	Printf(format string, args ...any)
}

type ResolverOptions struct {
	Logger Logger
}

type GitClient interface {
	IsGitRepo(ctx context.Context, repoDir string) (bool, error)
	Clone(ctx context.Context, url string, dest string) error
	CloneBare(ctx context.Context, url string, dest string, depth int) error
	Fetch(ctx context.Context, repoDir string, remote string, refspec string) error
	WorktreeAdd(ctx context.Context, repoDir string, worktreePath string, branch string) error
	WorktreeRemove(ctx context.Context, repoDir string, worktreePath string, force bool) error
	WorktreeList(ctx context.Context, repoDir string) ([]git.Worktree, error)
	HasWorktreeForBranch(ctx context.Context, repoDir string, branch string) (string, bool, error)
	OriginURL(ctx context.Context, repoDir string) (string, error)
	AddRemote(ctx context.Context, repoDir string, name string, url string) error
	HasRemote(ctx context.Context, repoDir string, name string) (bool, error)
	SetUpstream(ctx context.Context, repoDir string, branch string, upstream string) error
	ConfigSet(ctx context.Context, repoDir string, key string, value string) error
	ConfigSetWorktree(ctx context.Context, repoDir string, key string, value string) error
	WorktreeAddBranch(ctx context.Context, repoDir string, worktreePath string, branch string, startPoint string, force bool) error
}

func NewResolver(client GitClient, opts ResolverOptions) *Resolver {
	return &Resolver{git: client, logger: opts.Logger}
}

func (r *Resolver) Resolve(ctx context.Context, cfg config.Config, pr github.PRMetadata, opts Options) (Result, error) {
	if opts.Temp {
		return r.resolveTemp(ctx, cfg, pr)
	}
	return r.resolvePersistent(ctx, cfg, pr)
}

func (r *Resolver) resolvePersistent(ctx context.Context, cfg config.Config, pr github.PRMetadata) (Result, error) {
	repoDir, err := resolveRepoDir(ctx, r.git, cfg.ProjectsDir, pr.BaseRepo, r.logger)
	if err != nil {
		return Result{}, err
	}

	if err := ensureRepo(ctx, r.git, repoDir, pr.BaseRepo.CloneURL); err != nil {
		return Result{}, err
	}

	if isCrossRepo(pr) {
		if err := ensureRemote(ctx, r.git, repoDir, forkRemoteName(pr), pr.HeadRepo.CloneURL); err != nil {
			return Result{}, err
		}
	}

	branchRef := branchRefForPR(pr)
	if path, ok, err := r.git.HasWorktreeForBranch(ctx, repoDir, branchRef); err != nil {
		return Result{}, err
	} else if ok {
		result := Result{Path: path, RepoDir: repoDir, Reused: true}
		if err := fetchPR(ctx, r.git, repoDir, pr); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("fetch failed for existing worktree (working offline?): %v", err))
		}
		if err := r.ensureReadyWorktree(ctx, repoDir, path, pr); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not update worktree tracking config: %v", err))
		}
		r.logWarnings(result.Warnings)
		return result, nil
	}

	if err := fetchPR(ctx, r.git, repoDir, pr); err != nil {
		return Result{}, err
	}

	worktreeRoot := repoDir + "-worktrees"
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("create worktree directory: %w", err)
	}

	worktreePath := filepath.Join(worktreeRoot, worktreeName(pr))
	if pathExists(worktreePath) {
		return Result{}, fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	startPoint := remoteRefForPR(pr)
	if err := r.git.WorktreeAddBranch(ctx, repoDir, worktreePath, branchRef, startPoint, false); err != nil {
		if !errors.Is(err, git.ErrBranchExists) {
			return Result{}, err
		}
		// Branch exists as a stale leftover (e.g. after manual worktree
		// cleanup). Since HasWorktreeForBranch already confirmed no worktree
		// is using it, force-reset the branch with -B.
		if err := r.git.WorktreeAddBranch(ctx, repoDir, worktreePath, branchRef, startPoint, true); err != nil {
			return Result{}, err
		}
	}

	if err := r.ensureReadyWorktree(ctx, repoDir, worktreePath, pr); err != nil {
		return Result{}, err
	}

	return Result{Path: worktreePath, RepoDir: repoDir}, nil
}

func (r *Resolver) resolveTemp(ctx context.Context, cfg config.Config, pr github.PRMetadata) (Result, error) {
	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create temp dir: %w", err)
	}

	repoSlug := repoSlug(pr.BaseRepo)
	bareDir := filepath.Join(cfg.TempDir, repoSlug+".git")

	if err := ensureBareRepo(ctx, r.git, bareDir, pr.BaseRepo.CloneURL); err != nil {
		return Result{}, err
	}

	if isCrossRepo(pr) {
		if err := ensureRemote(ctx, r.git, bareDir, forkRemoteName(pr), pr.HeadRepo.CloneURL); err != nil {
			return Result{}, err
		}
	}

	branchRef := branchRefForPR(pr)
	if path, ok, err := r.git.HasWorktreeForBranch(ctx, bareDir, branchRef); err != nil {
		return Result{}, err
	} else if ok {
		result := Result{Path: path, RepoDir: bareDir, Reused: true}
		if err := fetchPR(ctx, r.git, bareDir, pr); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("fetch failed for existing worktree (working offline?): %v", err))
		}
		if err := r.ensureReadyWorktree(ctx, bareDir, path, pr); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not update worktree tracking config: %v", err))
		}
		r.logWarnings(result.Warnings)
		return result, nil
	}

	if err := fetchPR(ctx, r.git, bareDir, pr); err != nil {
		return Result{}, err
	}

	worktreePath := filepath.Join(cfg.TempDir, repoSlug+"-"+worktreeName(pr))
	if pathExists(worktreePath) {
		return Result{}, fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	startPoint := remoteRefForPR(pr)
	if err := r.git.WorktreeAddBranch(ctx, bareDir, worktreePath, branchRef, startPoint, true); err != nil {
		return Result{}, err
	}

	if err := r.ensureReadyWorktree(ctx, bareDir, worktreePath, pr); err != nil {
		return Result{}, err
	}

	return Result{Path: worktreePath, RepoDir: bareDir}, nil
}

func (r *Resolver) logWarnings(warnings []string) {
	if r.logger == nil {
		return
	}
	for _, w := range warnings {
		r.logger.Printf("Warning: %s", w)
	}
}

func (r *Resolver) ensureReadyWorktree(ctx context.Context, repoDir string, worktreePath string, pr github.PRMetadata) error {
	branchRef := branchRefForPR(pr)
	upstream := remoteRefForPR(pr)

	if err := r.git.SetUpstream(ctx, worktreePath, branchRef, upstream); err != nil {
		return err
	}
	if !isCrossRepo(pr) {
		return nil
	}
	if err := r.git.ConfigSet(ctx, repoDir, "extensions.worktreeConfig", "true"); err != nil {
		return err
	}
	if err := r.git.ConfigSetWorktree(ctx, worktreePath, "push.default", "upstream"); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) CleanTemp(ctx context.Context, tempDir string, ttl time.Duration, removeAll bool, dryRun bool) ([]CleanResult, error) {
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read temp dir: %w", err)
	}

	var results []CleanResult
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".git") {
			continue
		}
		bareDir := filepath.Join(tempDir, entry.Name())
		if err := r.cleanBareRepo(ctx, bareDir, ttl, removeAll, dryRun, &results); err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (r *Resolver) cleanBareRepo(ctx context.Context, bareDir string, ttl time.Duration, removeAll bool, dryRun bool, results *[]CleanResult) error {
	worktrees, err := r.git.WorktreeList(ctx, bareDir)
	if err != nil {
		return err
	}

	now := time.Now()
	removed := make(map[string]struct{})
	for _, wt := range worktrees {
		if wt.Path == bareDir {
			continue
		}
		shouldRemove := removeAll
		if !shouldRemove {
			info, err := os.Stat(wt.Path)
			if err == nil {
				shouldRemove = now.Sub(info.ModTime()) >= ttl
			}
		}

		if shouldRemove {
			*results = append(*results, CleanResult{Path: wt.Path})
			removed[wt.Path] = struct{}{}
			if !dryRun {
				if err := r.git.WorktreeRemove(ctx, bareDir, wt.Path, true); err != nil {
					return err
				}
			}
		}
	}

	if dryRun {
		return nil
	}

	remaining := 0
	for _, wt := range worktrees {
		if wt.Path == bareDir {
			continue
		}
		if _, ok := removed[wt.Path]; ok {
			continue
		}
		remaining++
	}

	if remaining == 0 {
		if err := os.RemoveAll(bareDir); err != nil {
			return fmt.Errorf("remove bare repo: %w", err)
		}
	}

	return nil
}

func resolveRepoDir(ctx context.Context, client GitClient, projectsDir string, repo github.Repository, logger Logger) (string, error) {
	primary := filepath.Join(projectsDir, repo.Name)
	if pathExists(primary) {
		isRepo, err := client.IsGitRepo(ctx, primary)
		if err != nil {
			return "", err
		}
		if isRepo {
			origin, err := client.OriginURL(ctx, primary)
			if err != nil {
				if logger != nil {
					logger.Printf("Warning: repo at %s has no origin; using %s", primary, filepath.Join(projectsDir, repoSlug(repo)))
				}
				alternate := filepath.Join(projectsDir, repoSlug(repo))
				return alternate, nil
			}
			if repoMatchesOrigin(origin, repo) {
				return primary, nil
			}
			if logger != nil {
				logger.Printf("Warning: repo at %s has origin %s which does not match %s/%s; using %s", primary, origin, repo.Owner, repo.Name, filepath.Join(projectsDir, repoSlug(repo)))
			}
		}

		alternate := filepath.Join(projectsDir, repoSlug(repo))
		return alternate, nil
	}

	return primary, nil
}

func ensureRepo(ctx context.Context, client GitClient, repoDir string, cloneURL string) error {
	if !pathExists(repoDir) {
		if err := client.Clone(ctx, cloneURL, repoDir); err != nil {
			return err
		}
		return nil
	}

	isRepo, err := client.IsGitRepo(ctx, repoDir)
	if err != nil {
		return err
	}
	if !isRepo {
		return fmt.Errorf("path is not a git repository: %s", repoDir)
	}

	hasOrigin, err := client.HasRemote(ctx, repoDir, "origin")
	if err != nil {
		return err
	}
	if !hasOrigin {
		return client.AddRemote(ctx, repoDir, "origin", cloneURL)
	}

	return nil
}

func ensureBareRepo(ctx context.Context, client GitClient, bareDir string, cloneURL string) error {
	if !pathExists(bareDir) {
		if err := client.CloneBare(ctx, cloneURL, bareDir, 0); err != nil {
			return err
		}
	} else {
		isRepo, err := client.IsGitRepo(ctx, bareDir)
		if err != nil {
			return err
		}
		if !isRepo {
			return fmt.Errorf("path is not a git repository: %s", bareDir)
		}
	}

	return client.ConfigSet(ctx, bareDir, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

func fetchPR(ctx context.Context, client GitClient, repoDir string, pr github.PRMetadata) error {
	var remote string
	var refspec string

	if isCrossRepo(pr) {
		remote = forkRemoteName(pr)
		refspec = fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", pr.HeadRef, remote, pr.HeadRef)
	} else {
		remote = "origin"
		refspec = fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", pr.HeadRef, pr.HeadRef)
	}

	return client.Fetch(ctx, repoDir, remote, refspec)
}

func branchRefForPR(pr github.PRMetadata) string {
	if isCrossRepo(pr) {
		return fmt.Sprintf("pr/%d/%s", pr.Number, pr.HeadRef)
	}
	return pr.HeadRef
}

func remoteRefForPR(pr github.PRMetadata) string {
	if isCrossRepo(pr) {
		return fmt.Sprintf("%s/%s", forkRemoteName(pr), pr.HeadRef)
	}
	return fmt.Sprintf("origin/%s", pr.HeadRef)
}

func worktreeName(pr github.PRMetadata) string {
	return fmt.Sprintf("pr-%d-%s", pr.Number, sanitizeBranch(pr.HeadRef))
}

func repoSlug(repo github.Repository) string {
	return fmt.Sprintf("%s-%s", repo.Owner, repo.Name)
}

func sanitizeBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.ReplaceAll(branch, "/", "-")
	branch = strings.ReplaceAll(branch, " ", "-")
	branch = strings.ReplaceAll(branch, "..", "-")
	if branch == "" {
		return "branch"
	}
	return branch
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isCrossRepo(pr github.PRMetadata) bool {
	return !strings.EqualFold(pr.BaseRepo.Owner, pr.HeadRepo.Owner) || !strings.EqualFold(pr.BaseRepo.Name, pr.HeadRepo.Name)
}

func forkRemoteName(pr github.PRMetadata) string {
	return fmt.Sprintf("prt/%s/%s", pr.HeadRepo.Owner, pr.HeadRepo.Name)
}

func ensureRemote(ctx context.Context, client GitClient, repoDir string, name string, url string) error {
	hasRemote, err := client.HasRemote(ctx, repoDir, name)
	if err != nil {
		return err
	}
	if !hasRemote {
		return client.AddRemote(ctx, repoDir, name, url)
	}
	// Preserve existing URL — user may have SSH, insteadOf rewrites, or
	// other auth customizations that differ from the GitHub API CloneURL.
	return nil
}

func repoMatchesOrigin(origin string, repo github.Repository) bool {
	origin = strings.ToLower(origin)
	repoName := strings.ToLower(fmt.Sprintf("%s/%s", repo.Owner, repo.Name))

	if strings.Contains(origin, repoName) {
		return true
	}

	return false
}
