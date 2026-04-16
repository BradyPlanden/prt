package github

import (
	"context"
	"fmt"
	"testing"
)

func TestParsePRURL(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		owner  string
		repo   string
		number int
		ok     bool
	}{
		{
			name:   "basic",
			input:  "https://github.com/octo/repo/pull/15",
			owner:  "octo",
			repo:   "repo",
			number: 15,
			ok:     true,
		},
		{
			name:   "trailing slash",
			input:  "https://github.com/octo/repo/pull/15/",
			owner:  "octo",
			repo:   "repo",
			number: 15,
			ok:     true,
		},
		{
			name:  "invalid host",
			input: "https://gitlab.com/octo/repo/pull/15",
			ok:    false,
		},
		{
			name:  "missing number",
			input: "https://github.com/octo/repo/pull/",
			ok:    false,
		},
		{
			name:  "not a pull url",
			input: "https://github.com/octo/repo/issues/15",
			ok:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParsePRURL(tc.input)
			if tc.ok {
				if err != nil {
					t.Fatalf("expected success: %v", err)
				}
				if ref.Owner != tc.owner || ref.Repo != tc.repo || ref.Number != tc.number {
					t.Fatalf("unexpected parse: %+v", ref)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error for %s", tc.input)
			}
		})
	}
}

type metadataRunner struct {
	output string
	err    error
}

func (r metadataRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte(r.output), r.err
}

func TestFetchPRMetadataAllowsMissingHeadRepository(t *testing.T) {
	output := `{
		"number": 15,
		"title": "Fix it",
		"state": "MERGED",
		"url": "https://github.com/octo/repo/pull/15",
		"headRefName": "feature",
		"baseRefName": "main",
		"headRepository": null,
		"headRepositoryOwner": {"login": "forker", "name": "Forker"}
	}`
	client := NewClient(ClientOptions{Runner: metadataRunner{output: output}})

	meta, err := client.FetchPRMetadata(context.Background(), "https://github.com/octo/repo/pull/15")
	if err != nil {
		t.Fatalf("FetchPRMetadata: %v", err)
	}
	if !meta.HeadRepoMissing {
		t.Fatalf("expected missing head repository to be recorded")
	}
	if meta.HeadRepo.Owner != "forker" {
		t.Fatalf("expected fallback head owner forker, got %s", meta.HeadRepo.Owner)
	}
	if meta.HeadRepo.Name != "repo" {
		t.Fatalf("expected fallback head repo name repo, got %s", meta.HeadRepo.Name)
	}
	if meta.HeadRepo.CloneURL != "" {
		t.Fatalf("expected no clone URL for missing head repository, got %s", meta.HeadRepo.CloneURL)
	}
}

func TestFetchPRMetadataRejectsMalformedHeadRepository(t *testing.T) {
	output := `{
		"number": 15,
		"title": "Fix it",
		"state": "OPEN",
		"url": "https://github.com/octo/repo/pull/15",
		"headRefName": "feature",
		"baseRefName": "main",
		"headRepository": {"name": "", "nameWithOwner": "", "url": "", "owner": {"login": ""}},
		"headRepositoryOwner": null
	}`
	client := NewClient(ClientOptions{Runner: metadataRunner{output: output}})

	_, err := client.FetchPRMetadata(context.Background(), "https://github.com/octo/repo/pull/15")
	if err == nil {
		t.Fatal("expected malformed head repository metadata to fail")
	}
	if got := err.Error(); got == "" || got == fmt.Sprint(nil) {
		t.Fatalf("expected descriptive error, got %q", got)
	}
}
