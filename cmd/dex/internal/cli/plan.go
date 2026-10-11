package cli

import "github.com/spf13/cobra"

func newPlanCmd(dir *string, d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show pending infrastructure changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := d.newEngine(*dir)
			if err != nil {
				return err
			}
			plan, err := e.Plan(cmd.Context(), *dir)
			if err != nil {
				return err
			}
			renderPlan(cmd.OutOrStdout(), plan)
			return nil
		},
	}
}
