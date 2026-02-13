package cli

import (
	"fmt"
	"os"

	"github.com/BradyPlanden/prt/internal/config"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	Temp     bool
	Projects string
	NoTab    bool
	Verbose  bool
	Terminal string
	TempDir  string
	TempTTL  string
	Config   string
}

// Execute runs the root prt command.
func Execute(version string) error {
	cmd := newRootCommand(version)
	return cmd.Execute()
}

func newRootCommand(version string) *cobra.Command {
	opts := &rootOptions{}
	if version == "" {
		version = "dev"
	}

	cmd := &cobra.Command{
		Use:   "prt <PR-URL>",
		Short: "Open a GitHub PR in a new terminal tab",
		Example: "" +
			"  prt https://github.com/OWNER/REPO/pull/123\n" +
			"  prt https://github.com/OWNER/REPO/pull/123 --temp\n" +
			"  prt https://github.com/OWNER/REPO/pull/123 --no-tab\n" +
			"  prt clean --dry-run",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("missing PR URL argument (run 'prt --help')")
			}
			if len(args) > 1 {
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(cmd, opts, args[0])
		},
	}

	cmd.Flags().BoolVarP(&opts.Temp, "temp", "t", false, "Use a temporary worktree")
	cmd.Flags().StringVar(&opts.Projects, "dir", "", "Override projects directory")
	cmd.Flags().BoolVar(&opts.NoTab, "no-tab", false, "Print path instead of opening a tab")
	cmd.Flags().StringVar(&opts.Terminal, "terminal", "", "Override terminal (auto|iterm2|terminal)")
	cmd.PersistentFlags().BoolVar(&opts.Verbose, "verbose", false, "Enable verbose logging")
	cmd.PersistentFlags().StringVar(&opts.TempDir, "temp-dir", "", "Override temp directory")
	cmd.PersistentFlags().StringVar(&opts.TempTTL, "temp-ttl", "", "Override temp cleanup TTL (e.g. 24h)")
	cmd.PersistentFlags().StringVar(&opts.Config, "config", "", "Override config file path")

	cmd.Version = version
	cmd.SetVersionTemplate("prt version {{.Version}}\n")

	cmd.AddCommand(newVersionCommand(version))
	cmd.AddCommand(newCleanCommand(opts))

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	cmd.SetHelpTemplate(fmt.Sprintf("%s\n", cmd.HelpTemplate()))

	return cmd
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print prt version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "prt version %s\n", version)
		},
	}
}

func loadConfig(opts *rootOptions) (config.Config, error) {
	overrides := config.Overrides{
		ProjectsDir: opts.Projects,
		TempDir:     opts.TempDir,
		Terminal:    opts.Terminal,
		TempTTL:     opts.TempTTL,
		Verbose:     opts.Verbose,
		ConfigPath:  opts.Config,
	}
	return config.Load(overrides)
}
