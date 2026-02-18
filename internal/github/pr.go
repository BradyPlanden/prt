package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// PRRef identifies a pull request by repository and number.
type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

// Repository identifies a GitHub repository and clone URL.
type Repository struct {
	Owner    string
	Name     string
	URL      string
	CloneURL string
}

// PRMetadata contains pull request details required for worktree setup.
type PRMetadata struct {
	Number   int
	Title    string
	State    string
	URL      string
	HeadRef  string
	BaseRef  string
	BaseRepo Repository
	HeadRepo Repository
}

// Client fetches pull request metadata via the gh CLI.
type Client struct {
	runner  Runner
	verbose bool
}

// ClientOptions configures a GitHub metadata client.
type ClientOptions struct {
	Verbose bool
	Runner  Runner
}

// Runner executes external commands for metadata retrieval.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner executes commands via os/exec.
type ExecRunner struct{}

// Run executes a command and returns combined output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// NewClient constructs a Client using defaults when options are omitted.
func NewClient(opts ClientOptions) *Client {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Client{runner: runner, verbose: opts.Verbose}
}

// ParsePRURL parses a GitHub pull request URL into owner, repo, and number.
func ParsePRURL(prURL string) (PRRef, error) {
	parsed, err := url.Parse(prURL)
	if err != nil {
		return PRRef{}, fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(parsed.Host)
	if host == "" {
		return PRRef{}, errors.New("missing URL host")
	}
	if host != "github.com" && !strings.HasSuffix(host, ".github.com") {
		return PRRef{}, fmt.Errorf("unsupported host: %s", parsed.Host)
	}

	cleanPath := strings.TrimSuffix(parsed.Path, "/")
	parts := strings.Split(cleanPath, "/")
	if len(parts) < 5 {
		return PRRef{}, errors.New("expected /owner/repo/pull/number")
	}
	if parts[0] == "" {
		parts = parts[1:]
	}
	if len(parts) < 4 {
		return PRRef{}, errors.New("expected /owner/repo/pull/number")
	}

	owner := parts[0]
	repo := parts[1]
	if parts[2] != "pull" {
		return PRRef{}, errors.New("URL is not a pull request")
	}

	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return PRRef{}, errors.New("invalid pull request number")
	}

	return PRRef{Owner: owner, Repo: repo, Number: number}, nil
}

// FetchPRMetadata loads pull request metadata needed to resolve worktrees.
func (c *Client) FetchPRMetadata(ctx context.Context, prURL string) (PRMetadata, error) {
	ref, err := ParsePRURL(prURL)
	if err != nil {
		return PRMetadata{}, err
	}

	args := []string{
		"pr", "view", prURL,
		"--json", "number,title,state,url,headRefName,baseRefName,headRepository,headRepositoryOwner",
	}

	output, err := c.runner.Run(ctx, "gh", args...)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return PRMetadata{}, errors.New("gh CLI not found; install it from https://cli.github.com/")
		}
		return PRMetadata{}, fmt.Errorf("gh pr view failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	var payload ghPR
	if err := json.Unmarshal(output, &payload); err != nil {
		return PRMetadata{}, fmt.Errorf("parse gh output: %w", err)
	}

	baseRepo := Repository{
		Owner:    ref.Owner,
		Name:     ref.Repo,
		URL:      fmt.Sprintf("https://github.com/%s/%s", ref.Owner, ref.Repo),
		CloneURL: fmt.Sprintf("https://github.com/%s/%s.git", ref.Owner, ref.Repo),
	}

	headRepo, err := repoFromHeadPayload(payload.HeadRepository, payload.HeadRepositoryOwner)
	if err != nil {
		return PRMetadata{}, fmt.Errorf("head repository: %w", err)
	}

	return PRMetadata{
		Number:   payload.Number,
		Title:    payload.Title,
		State:    payload.State,
		URL:      payload.URL,
		HeadRef:  payload.HeadRefName,
		BaseRef:  payload.BaseRefName,
		BaseRepo: baseRepo,
		HeadRepo: headRepo,
	}, nil
}

type ghPR struct {
	Number              int          `json:"number"`
	Title               string       `json:"title"`
	State               string       `json:"state"`
	URL                 string       `json:"url"`
	HeadRefName         string       `json:"headRefName"`
	BaseRefName         string       `json:"baseRefName"`
	HeadRepository      *ghRepo      `json:"headRepository"`
	HeadRepositoryOwner *ghRepoOwner `json:"headRepositoryOwner"`
}

type ghRepo struct {
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type ghRepoOwner struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

func repoFromHeadPayload(repo *ghRepo, owner *ghRepoOwner) (Repository, error) {
	if repo == nil {
		return Repository{}, errors.New("repository not found")
	}

	ownerLogin := repo.Owner.Login
	name := repo.Name
	if ownerLogin == "" && owner != nil {
		ownerLogin = owner.Login
	}
	if ownerLogin == "" || name == "" {
		if repo.NameWithOwner != "" {
			parts := strings.Split(repo.NameWithOwner, "/")
			if len(parts) == 2 {
				ownerLogin = parts[0]
				name = parts[1]
			}
		}
	}
	if ownerLogin == "" || name == "" {
		return Repository{}, errors.New("missing repository owner or name")
	}

	cloneURL := repo.URL
	if cloneURL == "" {
		cloneURL = fmt.Sprintf("https://github.com/%s/%s", ownerLogin, name)
	}
	cloneURL = ensureGitSuffix(cloneURL)

	return Repository{
		Owner:    ownerLogin,
		Name:     name,
		URL:      repo.URL,
		CloneURL: cloneURL,
	}, nil
}

func ensureGitSuffix(value string) string {
	if strings.HasSuffix(value, ".git") {
		return value
	}
	return value + ".git"
}
