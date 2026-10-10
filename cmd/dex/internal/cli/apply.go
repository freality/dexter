package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newApplyCmd(dir *string, d deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply pending infrastructure changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			entry, err := entrypointPath(*dir)
			if err != nil {
				return err
			}
			e, err := d.newEngine(*dir)
			if err != nil {
				return err
			}

			plan, err := e.Plan(cmd.Context(), entry)
			if err != nil {
				return err
			}
			renderPlan(cmd.OutOrStdout(), plan)
			if plan.Empty() {
				return nil
			}

			if !yes {
				fmt.Fprint(cmd.OutOrStdout(), "Apply these changes? [y/N]: ")
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				answer := strings.ToLower(strings.TrimSpace(line))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Apply cancelled.")
					return nil
				}
			}

			applied, err := e.Apply(cmd.Context(), entry)
			if err != nil {
				return err
			}
			add, change, destroy := applied.Counts()
			fmt.Fprintf(cmd.OutOrStdout(), "Apply complete: %d added, %d changed, %d destroyed.\n", add, change, destroy)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "apply without interactive confirmation")
	return cmd
}
