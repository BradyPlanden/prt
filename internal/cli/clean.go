package cli

import (
	"fmt"
	"log"
	"time"

	"github.com/BradyPlanden/prt/internal/git"
	"github.com/BradyPlanden/prt/internal/workspace"
	"github.com/spf13/cobra"
)

type cleanOptions struct {
	All    bool
	DryRun bool
}

func newCleanCommand(rootOpts *rootOptions) *cobra.Command {
	opts := &cleanOptions{}

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove old temporary worktrees",
		Example: "" +
			"  prt clean --dry-run\n" +
			"  prt clean --all",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClean(cmd, rootOpts, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Remove all temp worktrees")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be removed")

	return cmd
}

func runClean(cmd *cobra.Command, rootOpts *rootOptions, opts *cleanOptions) error {
	cfg, err := loadConfig(rootOpts)
	if err != nil {
		return err
	}

	logger := log.New(cmd.ErrOrStderr(), "", 0)
	gitClient := git.NewClient(git.ClientOptions{
		Verbose: cfg.Verbose,
		Logger:  logger,
	})
	resolver := workspace.NewResolver(gitClient, workspace.ResolverOptions{
		Logger: logger,
	})

	ctx, cancel := withDefaultTimeout(cmd.Context())
	defer cancel()

	var ttl time.Duration
	if !opts.All {
		ttl = cfg.TempTTL
	}

	results, err := resolver.CleanTemp(ctx, cfg.TempDir, ttl, opts.All, opts.DryRun)
	if err != nil {
		return err
	}

	for _, result := range results {
		switch result.Action {
		case workspace.CleanActionWouldRemove:
			fmt.Fprintf(cmd.OutOrStdout(), "Would remove %s\n", result.Path)
		case workspace.CleanActionRemoved:
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", result.Path)
		case workspace.CleanActionWouldPrune:
			fmt.Fprintf(cmd.OutOrStdout(), "Would prune missing worktree metadata for %s\n", result.Path)
		case workspace.CleanActionPruned:
			fmt.Fprintf(cmd.OutOrStdout(), "Pruned missing worktree metadata for %s\n", result.Path)
		case workspace.CleanActionSkipped:
			if result.Reason == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Skipped %s\n", result.Path)
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Skipped %s (%s)\n", result.Path, result.Reason)
		}
	}

	return nil
}
