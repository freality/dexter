package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	term "github.com/charmbracelet/x/term"

	"github.com/spf13/cobra"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/diff"
)

func newApplyCmd(dir *string, d deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply pending infrastructure changes",
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

			w := cmd.OutOrStdout()
			applied, err := runApply(cmd.Context(), e, plan, w)
			if err != nil {
				return err
			}
			add, change, destroy := applied.Counts()
			fmt.Fprintf(w, "Apply complete: %d added, %d changed, %d destroyed.\n", add, change, destroy)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "apply without interactive confirmation")
	return cmd
}

// runApply dispatches to the TUI or plain sink based on whether w is a
// terminal. The confirmation prompt has already fired.
func runApply(ctx context.Context, e *engine.Engine, plan *diff.Plan, w io.Writer) (*diff.Plan, error) {
	if isTerminal(w) {
		return runApplyTUI(ctx, e, plan, w)
	}
	e.SetEvents(applySink(w))
	return e.Apply(ctx, plan)
}

// termIsTerminal is a seam over term.IsTerminal for tests (mirrors
// cmd/dex/internal/cli/plugins.go's pluginOpen seam): real terminal fds
// can't be fabricated, so tests stub the check instead.
var termIsTerminal = term.IsTerminal

// isTerminal reports whether w is connected to a terminal.
func isTerminal(w io.Writer) bool {
	if f, ok := w.(term.File); ok {
		return termIsTerminal(f.Fd())
	}
	return false
}

// ttyInput overrides the TUI Program's input source in tests; nil (the
// default) leaves bubbletea reading the real stdin, which production needs
// for Ctrl+C to reach ApplyModel.Update as a tea.KeyMsg.
var ttyInput io.Reader

// runProgram is a seam over (*tea.Program).Run for tests: forcing a real
// bubbletea internal error (distinct from the expected kill/interrupt) isn't
// reliably reproducible from outside the library, so tests stub this
// instead (same pattern as termIsTerminal and pluginOpen).
var runProgram = func(p *tea.Program) (tea.Model, error) { return p.Run() }

// runApplyTUI runs the TUI-driven apply.
func runApplyTUI(ctx context.Context, e *engine.Engine, plan *diff.Plan, w io.Writer) (*diff.Plan, error) {
	model := NewApplyModel()
	opts := []tea.ProgramOption{tea.WithOutput(w), tea.WithoutSignalHandler(), tea.WithoutSignals()}
	if ttyInput != nil {
		opts = append(opts, tea.WithInput(ttyInput))
	}
	p := tea.NewProgram(model, opts...)

	e.SetEvents(func(ev engine.Event) {
		p.Send(EventMsg{Event: ev})
	})

	var (
		applied  *diff.Plan
		applyErr error
	)
	go func() {
		defer p.Quit()
		applied, applyErr = e.Apply(ctx, plan)
	}()

	if _, err := runProgram(p); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return nil, err
	}
	if applyErr != nil {
		return applied, applyErr
	}
	fmt.Fprintln(w, model.Summary())
	return applied, nil
}
