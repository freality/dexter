package cli

import "github.com/spf13/cobra"

func newInitCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new dex project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.OutOrStdout(), *dir)
		},
	}
}
