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

func Execute() error {
	cmd := newRootCommand()
	return cmd.Execute()
}

func newRootCommand() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:   "prt <PR-URL>",
		Short: "Open a GitHub PR in a new terminal tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(cmd, opts, args[0])
		},
	}

	cmd.PersistentFlags().BoolVarP(&opts.Temp, "temp", "t", false, "Use a temporary worktree")
	cmd.PersistentFlags().StringVar(&opts.Projects, "dir", "", "Override projects directory")
	cmd.PersistentFlags().BoolVar(&opts.NoTab, "no-tab", false, "Print path instead of opening a tab")
	cmd.PersistentFlags().BoolVar(&opts.Verbose, "verbose", false, "Enable verbose logging")
	cmd.PersistentFlags().StringVar(&opts.Terminal, "terminal", "", "Override terminal (auto|iterm2|terminal)")
	cmd.PersistentFlags().StringVar(&opts.TempDir, "temp-dir", "", "Override temp directory")
	cmd.PersistentFlags().StringVar(&opts.TempTTL, "temp-ttl", "", "Override temp cleanup TTL (e.g. 24h)")
	cmd.PersistentFlags().StringVar(&opts.Config, "config", "", "Override config file path")

	cmd.AddCommand(newCleanCommand(opts))

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	cmd.SetHelpTemplate(fmt.Sprintf("%s\n", cmd.HelpTemplate()))

	return cmd
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
