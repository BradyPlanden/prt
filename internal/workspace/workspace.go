package workspace

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BradyPlanden/prt/internal/config"
	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/github"
)

// Options controls resolver behavior for temp versus persistent worktrees.
type Options struct {
	Temp bool
}

// Result is the resolved workspace location and related metadata.
type Result struct {
	Path     string
	RepoDir  string
	Reused   bool
	Warnings []string
}

// CleanResult describes one removed or removable worktree path.
type CleanResult struct {
	Path   string
	Action CleanAction
	Reason string
}

// CleanAction describes the outcome for a temp worktree cleanup candidate.
type CleanAction string

const (
	CleanActionRemoved     CleanAction = "removed"
	CleanActionWouldRemove CleanAction = "would_remove"
	CleanActionPruned      CleanAction = "pruned"
	CleanActionWouldPrune  CleanAction = "would_prune"
	CleanActionSkipped     CleanAction = "skipped"
)

// Resolver maps PR metadata to persistent or temporary worktrees.
type Resolver struct {
	git    GitClient
	logger Logger
}

// Logger provides warning output hooks.
type Logger interface {
	Printf(format string, args ...any)
}

// ResolverOptions configures optional resolver behavior.
type ResolverOptions struct {
	Logger Logger
}

// GitClient defines the git operations required by Resolver.
type GitClient interface {
	IsGitRepo(ctx context.Context, repoDir string) (bool, error)
	Clone(ctx context.Context, url string, dest string) error
	CloneBare(ctx context.Context, url string, dest string, depth int) error
	Fetch(ctx context.Context, repoDir string, remote string, refspec string) error
	FetchBranch(ctx context.Context, repoDir string, remote string, branch string) error
	SubmoduleUpdate(ctx context.Context, repoDir string) error
	WorktreeAdd(ctx context.Context, repoDir string, worktreePath string, branch string) error
	WorktreeRemove(ctx context.Context, repoDir string, worktreePath string, force bool) error
	WorktreeList(ctx context.Context, repoDir string) ([]git.Worktree, error)
	HasWorktreeForBranch(ctx context.Context, repoDir string, branch string) (string, bool, error)
	OriginURL(ctx context.Context, repoDir string) (string, error)
	RemoteURL(ctx context.Context, repoDir string, name string) (string, error)
	AddRemote(ctx context.Context, repoDir string, name string, url string) error
	HasRemote(ctx context.Context, repoDir string, name string) (bool, error)
	SetUpstream(ctx context.Context, repoDir string, branch string, upstream string) error
	ConfigSet(ctx context.Context, repoDir string, key string, value string) error
	ConfigSetWorktree(ctx context.Context, repoDir string, key string, value string) error
	WorktreeAddBranch(ctx context.Context, repoDir string, worktreePath string, branch string, startPoint string, force bool) error
	IsWorktreeDirty(ctx context.Context, repoDir string) (bool, error)
	WorktreePrune(ctx context.Context, repoDir string) error
}

// NewResolver constructs a Resolver with the provided git client.
func NewResolver(client GitClient, opts ResolverOptions) *Resolver {
	return &Resolver{git: client, logger: opts.Logger}
}

// Resolve returns an existing or newly created worktree for a PR.
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

	worktreePath := filepath.Join(repoDir+"-worktrees", worktreeName(pr))
	return r.resolveWorktree(ctx, repoDir, worktreePath, pr, false)
}

func (r *Resolver) resolveTemp(ctx context.Context, cfg config.Config, pr github.PRMetadata) (Result, error) {
	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create temp dir: %w", err)
	}

	slug := repoSlug(pr.BaseRepo)
	bareDir := filepath.Join(cfg.TempDir, slug+".git")

	if err := ensureBareRepo(ctx, r.git, bareDir, pr.BaseRepo.CloneURL); err != nil {
		return Result{}, err
	}

	worktreePath := filepath.Join(cfg.TempDir, slug+"-"+worktreeName(pr))
	result, err := r.resolveWorktree(ctx, bareDir, worktreePath, pr, true)
	if err != nil {
		return Result{}, err
	}
	if err := touchTempWorktreeMarker(cfg.TempDir, result.Path); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not update temp worktree usage marker: %v", err))
		r.logWarnings([]string{fmt.Sprintf("could not update temp worktree usage marker: %v", err)})
	}
	return result, nil
}

// resolveWorktree handles the fetch/check/create cycle shared by persistent
// and temp modes. repoDir is the bare or non-bare repository, worktreePath is
// the desired worktree location, and alwaysForce skips stale-branch recovery
// by always using -B on worktree creation (used for temp mode).
func (r *Resolver) resolveWorktree(ctx context.Context, repoDir string, worktreePath string, pr github.PRMetadata, alwaysForce bool) (Result, error) {
	if canUseHeadRemote(pr) && isCrossRepo(pr) {
		if err := ensureRemote(ctx, r.git, repoDir, forkRemoteName(pr), pr.HeadRepo.CloneURL); err != nil {
			return Result{}, err
		}
	}

	// Keep the PR's target branch up to date for accurate local diffs.
	var warnings []string
	if pr.BaseRef != "" {
		if err := r.git.FetchBranch(ctx, repoDir, "origin", pr.BaseRef); err != nil {
			// Non-fatal: stale base is inconvenient but not blocking.
			warnings = append(warnings, fmt.Sprintf("could not fetch base branch %s (working offline?): %v", pr.BaseRef, err))
		}
	}

	branchRef := branchRefForPR(pr)
	if path, ok, err := r.git.HasWorktreeForBranch(ctx, repoDir, branchRef); err != nil {
		return Result{}, err
	} else if ok {
		result := Result{Path: path, RepoDir: repoDir, Reused: true, Warnings: warnings}
		target, err := fetchPR(ctx, r.git, repoDir, pr)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("fetch failed for existing worktree (working offline?): %v", err))
		}
		wtWarnings, err := r.ensureReadyWorktree(ctx, repoDir, path, pr, target)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not update worktree tracking config: %v", err))
		}
		result.Warnings = append(result.Warnings, wtWarnings...)
		r.logWarnings(result.Warnings)
		return result, nil
	}

	target, err := fetchPR(ctx, r.git, repoDir, pr)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create worktree directory: %w", err)
	}
	if pathExists(worktreePath) {
		return Result{}, fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	startPoint := target.StartPoint
	if alwaysForce {
		if err := r.git.WorktreeAddBranch(ctx, repoDir, worktreePath, branchRef, startPoint, true); err != nil {
			return Result{}, err
		}
	} else {
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
	}

	wtWarnings, err := r.ensureReadyWorktree(ctx, repoDir, worktreePath, pr, target)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, wtWarnings...)

	result := Result{Path: worktreePath, RepoDir: repoDir, Warnings: warnings}
	r.logWarnings(result.Warnings)
	return result, nil
}

func (r *Resolver) logWarnings(warnings []string) {
	if r.logger == nil {
		return
	}
	for _, w := range warnings {
		r.logger.Printf("Warning: %s", w)
	}
}

func (r *Resolver) ensureReadyWorktree(ctx context.Context, repoDir string, worktreePath string, pr github.PRMetadata, target prCheckoutTarget) ([]string, error) {
	branchRef := branchRefForPR(pr)

	if target.Upstream != "" {
		if err := r.git.SetUpstream(ctx, worktreePath, branchRef, target.Upstream); err != nil {
			return nil, err
		}
	}
	if isCrossRepo(pr) && !target.IsPullRef {
		if err := r.git.ConfigSet(ctx, repoDir, "extensions.worktreeConfig", "true"); err != nil {
			return nil, err
		}
		if err := r.git.ConfigSetWorktree(ctx, worktreePath, "push.default", "upstream"); err != nil {
			return nil, err
		}
	}

	// Initialize submodules in the worktree (best-effort).
	var warnings []string
	if err := r.git.SubmoduleUpdate(ctx, worktreePath); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not initialize submodules: %v", err))
	}

	return warnings, nil
}

// CleanTemp removes temp worktrees in tempDir based on ttl and options.
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

	tempDir := filepath.Dir(bareDir)
	now := time.Now()
	removed := make(map[string]struct{})
	prunedMissing := false
	for _, wt := range worktrees {
		if wt.Path == bareDir {
			continue
		}

		_, err := os.Stat(wt.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				action := CleanActionPruned
				if dryRun {
					action = CleanActionWouldPrune
				}
				*results = append(*results, CleanResult{
					Path:   wt.Path,
					Action: action,
					Reason: "worktree path is missing",
				})
				removed[wt.Path] = struct{}{}
				prunedMissing = true
				if !dryRun {
					if err := removeTempWorktreeMarker(tempDir, wt.Path); err != nil {
						return err
					}
				}
				continue
			}
			return fmt.Errorf("stat worktree: %w", err)
		}

		shouldRemove := removeAll
		if !shouldRemove {
			lastUsed, ok, err := tempWorktreeLastUsedAt(tempDir, wt.Path)
			if err != nil {
				return err
			}
			if !ok {
				info, err := os.Stat(wt.Path)
				if err != nil {
					return fmt.Errorf("stat worktree for legacy cleanup fallback: %w", err)
				}
				lastUsed = info.ModTime()
			}
			shouldRemove = now.Sub(lastUsed) >= ttl
		}

		if !shouldRemove {
			continue
		}

		if !removeAll {
			dirty, err := r.git.IsWorktreeDirty(ctx, wt.Path)
			if err != nil {
				return err
			}
			if dirty {
				*results = append(*results, CleanResult{
					Path:   wt.Path,
					Action: CleanActionSkipped,
					Reason: "worktree has uncommitted changes",
				})
				continue
			}
		}

		action := CleanActionRemoved
		if dryRun {
			action = CleanActionWouldRemove
		}
		*results = append(*results, CleanResult{Path: wt.Path, Action: action})
		removed[wt.Path] = struct{}{}
		if !dryRun {
			if err := r.git.WorktreeRemove(ctx, bareDir, wt.Path, true); err != nil {
				return err
			}
			if err := removeTempWorktreeMarker(tempDir, wt.Path); err != nil {
				return err
			}
		}
	}

	if prunedMissing && !dryRun {
		if err := r.git.WorktreePrune(ctx, bareDir); err != nil {
			return err
		}
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

	if dryRun {
		return nil
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
	originURL, err := client.OriginURL(ctx, repoDir)
	if err != nil {
		return err
	}
	if !remotesMatchRepo(originURL, cloneURL) {
		return fmt.Errorf("existing origin %q does not match expected repository %q", originURL, cloneURL)
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

	hasOrigin, err := client.HasRemote(ctx, bareDir, "origin")
	if err != nil {
		return err
	}
	if !hasOrigin {
		if err := client.AddRemote(ctx, bareDir, "origin", cloneURL); err != nil {
			return err
		}
	} else {
		originURL, err := client.OriginURL(ctx, bareDir)
		if err != nil {
			return err
		}
		if !remotesMatchRepo(originURL, cloneURL) {
			return fmt.Errorf("existing origin %q does not match expected repository %q", originURL, cloneURL)
		}
	}

	return client.ConfigSet(ctx, bareDir, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

func fetchPR(ctx context.Context, client GitClient, repoDir string, pr github.PRMetadata) (prCheckoutTarget, error) {
	target := primaryCheckoutTarget(pr)
	if err := client.Fetch(ctx, repoDir, target.Remote, target.Refspec); err == nil {
		return target, nil
	} else if !shouldFallbackToPullRef(pr, target) {
		return target, err
	} else {
		fallback := pullRefCheckoutTarget(pr)
		if fallbackErr := client.Fetch(ctx, repoDir, fallback.Remote, fallback.Refspec); fallbackErr != nil {
			return target, fmt.Errorf("direct fetch failed: %w; fallback pull ref fetch failed: %v", err, fallbackErr)
		}
		return fallback, nil
	}
}

func branchRefForPR(pr github.PRMetadata) string {
	if isCrossRepo(pr) {
		return fmt.Sprintf("pr/%d/%s", pr.Number, pr.HeadRef)
	}
	return pr.HeadRef
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

func canUseHeadRemote(pr github.PRMetadata) bool {
	return !pr.HeadRepoMissing && pr.HeadRepo.CloneURL != ""
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
	existingURL, err := client.RemoteURL(ctx, repoDir, name)
	if err != nil {
		return err
	}
	if !remotesMatchRepo(existingURL, url) {
		return fmt.Errorf("existing remote %q points to %q, expected %q", name, existingURL, url)
	}
	// Preserve existing URL — user may have SSH, insteadOf rewrites, or
	// other auth customizations that differ from the GitHub API CloneURL.
	return nil
}

func repoMatchesOrigin(origin string, repo github.Repository) bool {
	repoPath := strings.ToLower(fmt.Sprintf("%s/%s", repo.Owner, repo.Name))
	return repoPathFromRemote(origin) == repoPath
}

func remotesMatchRepo(remoteURL string, expectedURL string) bool {
	remoteRepo := repoPathFromRemote(remoteURL)
	expectedRepo := repoPathFromRemote(expectedURL)
	return remoteRepo != "" && remoteRepo == expectedRepo
}

func repoPathFromRemote(remote string) string {
	remote = strings.TrimSpace(strings.ToLower(remote))
	remote = strings.TrimSuffix(remote, ".git")
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "ssh://") || strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://") {
		if parsed, err := url.Parse(remote); err == nil {
			return strings.TrimPrefix(parsed.Path, "/")
		}
	}
	if idx := strings.Index(remote, ":"); idx >= 0 {
		return strings.TrimPrefix(remote[idx+1:], "/")
	}
	if idx := strings.Index(remote, "/"); idx >= 0 {
		return strings.TrimPrefix(remote[idx:], "/")
	}
	return ""
}

type prCheckoutTarget struct {
	Remote    string
	Refspec   string
	StartPoint string
	Upstream  string
	IsPullRef bool
}

func primaryCheckoutTarget(pr github.PRMetadata) prCheckoutTarget {
	if pr.HeadRepoMissing {
		return pullRefCheckoutTarget(pr)
	}
	if isCrossRepo(pr) {
		remote := forkRemoteName(pr)
		upstream := fmt.Sprintf("%s/%s", remote, pr.HeadRef)
		return prCheckoutTarget{
			Remote:     remote,
			Refspec:    fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", pr.HeadRef, remote, pr.HeadRef),
			StartPoint: upstream,
			Upstream:   upstream,
		}
	}
	upstream := fmt.Sprintf("origin/%s", pr.HeadRef)
	return prCheckoutTarget{
		Remote:     "origin",
		Refspec:    fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", pr.HeadRef, pr.HeadRef),
		StartPoint: upstream,
		Upstream:   upstream,
	}
}

func pullRefCheckoutTarget(pr github.PRMetadata) prCheckoutTarget {
	remoteRef := fmt.Sprintf("origin/prt/pull/%d/head", pr.Number)
	return prCheckoutTarget{
		Remote:     "origin",
		Refspec:    fmt.Sprintf("+refs/pull/%d/head:refs/remotes/%s", pr.Number, remoteRef),
		StartPoint: remoteRef,
		IsPullRef:  true,
	}
}

func shouldFallbackToPullRef(pr github.PRMetadata, target prCheckoutTarget) bool {
	if target.IsPullRef {
		return false
	}
	if pr.HeadRepoMissing {
		return true
	}
	return strings.EqualFold(pr.State, "closed") || strings.EqualFold(pr.State, "merged")
}

func tempWorktreeMarkerPath(tempDir string, worktreePath string) string {
	sum := sha256.Sum256([]byte(worktreePath))
	name := fmt.Sprintf("%s-%x.last-used", filepath.Base(worktreePath), sum[:8])
	return filepath.Join(tempDir, ".prt-meta", "last-used", name)
}

func touchTempWorktreeMarker(tempDir string, worktreePath string) error {
	now := time.Now()
	path := tempWorktreeMarkerPath(tempDir, worktreePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create usage marker directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open usage marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close usage marker: %w", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("touch usage marker: %w", err)
	}
	return nil
}

func tempWorktreeLastUsedAt(tempDir string, worktreePath string) (time.Time, bool, error) {
	info, err := os.Stat(tempWorktreeMarkerPath(tempDir, worktreePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("stat usage marker: %w", err)
	}
	return info.ModTime(), true, nil
}

func removeTempWorktreeMarker(tempDir string, worktreePath string) error {
	err := os.Remove(tempWorktreeMarkerPath(tempDir, worktreePath))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove usage marker: %w", err)
}
