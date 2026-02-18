package cli

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/github"
	"github.com/BradyPlanden/prt/internal/terminal"
	"github.com/BradyPlanden/prt/internal/workspace"
	"github.com/spf13/cobra"
)

func runOpen(cmd *cobra.Command, opts *rootOptions, prURL string) error {
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}

	ctx, cancel := withDefaultTimeout(cmd.Context())
	defer cancel()

	ghClient := github.NewClient(github.ClientOptions{Verbose: cfg.Verbose})
	meta, err := ghClient.FetchPRMetadata(ctx, prURL)
	if err != nil {
		return err
	}

	if strings.EqualFold(meta.State, "CLOSED") || strings.EqualFold(meta.State, "MERGED") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: PR is %s: %s\n", strings.ToUpper(meta.State), meta.URL)
	}

	logger := log.New(cmd.ErrOrStderr(), "", 0)
	gitClient := git.NewClient(git.ClientOptions{
		Verbose: cfg.Verbose,
		Logger:  logger,
	})

	resolver := workspace.NewResolver(gitClient, workspace.ResolverOptions{
		Logger: logger,
	})
	result, err := resolver.Resolve(ctx, cfg, meta, workspace.Options{Temp: opts.Temp})
	if err != nil {
		return err
	}

	if opts.NoTab {
		fmt.Fprintln(cmd.OutOrStdout(), result.Path)
		return nil
	}

	termCfg := terminal.Config{Terminal: cfg.Terminal}
	opener, err := terminal.Detect(termCfg)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Terminal detection failed: %v\n", err)
		fmt.Fprintln(cmd.OutOrStdout(), result.Path)
		return nil
	}

	if err := opener.Open(result.Path); err != nil {
		var permErr terminal.PermissionError
		if errors.As(err, &permErr) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Automation permission error: %v\n", permErr)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "Failed to open terminal tab: %v\n", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), result.Path)
		return nil
	}

	return nil
}
