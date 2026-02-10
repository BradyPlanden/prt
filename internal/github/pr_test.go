package github

import "testing"

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
