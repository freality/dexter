package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/freality/dexter/pkg/jsenv"
)

// runner is the seam between `dex run` and the evaluation machinery; the
// production implementation bundles with esbuild and evaluates with goja.
type runner interface {
	List(ctx context.Context) ([]jsenv.CommandInfo, error)
	Run(ctx context.Context, name string, args []string) (int, error)
}

// commandRunner implements runner against a project directory.
type commandRunner struct {
	bundler   jsenv.Bundler
	evaluator *jsenv.GojaEvaluator
	dir       string
	env       map[string]string
	resources func() (map[string]any, error)
	exec      jsenv.ExecFunc
}

// newRunner wires the production runner: .env merged with process env, state
// read from disk, host-process exec, and no eval budget (D13: a command is
// user-invoked day-two ops, not bounded synthesis).
func newRunner(dir string) (runner, error) {
	env, err := loadEnv(dir)
	if err != nil {
		return nil, err
	}
	mergeProcessEnv(env)
	return &commandRunner{
		bundler:   jsenv.NewEsbuildBundler(),
		evaluator: jsenv.NewGojaEvaluatorWithTimeout(0),
		dir:       dir,
		env:       env,
		resources: func() (map[string]any, error) { return loadStateResources(dir) },
		exec:      hostExec,
	}, nil
}

// bundle discovers and bundles the project's TypeScript.
func (r *commandRunner) bundle(ctx context.Context) (string, error) {
	files, err := jsenv.Discover(r.dir)
	if err != nil {
		return "", err
	}
	return r.bundler.Bundle(r.dir, files)
}

// List evaluates the project and returns its registered commands.
func (r *commandRunner) List(ctx context.Context) ([]jsenv.CommandInfo, error) {
	script, err := r.bundle(ctx)
	if err != nil {
		return nil, err
	}
	return r.evaluator.ListCommands(ctx, script, r.env)
}

// Run evaluates the project and runs the command named name with args.
func (r *commandRunner) Run(ctx context.Context, name string, args []string) (int, error) {
	script, err := r.bundle(ctx)
	if err != nil {
		return 0, err
	}
	resources, err := r.resources()
	if err != nil {
		return 0, fmt.Errorf("load state: %w", err)
	}
	return r.evaluator.RunCommand(ctx, script, r.env, name, args, resources, r.exec)
}

// loadStateResources returns state.json's resources re-serialized exactly as
// persisted: secret properties stay {token, sha256}, never plaintext (D13).
// A project that was never applied has no state file and yields {}.
func loadStateResources(dir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ".dex", "state.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	// Only the resources subtree: state.json is the source of truth, so the
	// properties are exactly what was persisted (hashed secrets included).
	var st struct {
		Resources map[string]any `json:"resources"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", dir, err)
	}
	if st.Resources == nil {
		return map[string]any{}, nil
	}
	return st.Resources, nil
}

// hostExec runs argv as a child of the dex process with stdio attached and
// returns its exit code. An error means the process could not be started or
// was killed by a signal mid-run; a child that ran and exited non-zero is an
// exit code, not an error.
func hostExec(ctx context.Context, argv []string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("empty command line")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, err
	}
	if exitErr.Exited() {
		return exitErr.ExitCode(), nil
	}
	// Killed by a signal (SIGINT via ctx cancellation): surface as an error
	// so the command is interrupted, never silently completed.
	return 0, errors.New("interrupted by signal")
}

// exitCodeError carries a command's exit code out of RunE so Main can use it
// as the process exit code without printing an Error line.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// newRunCmd implements `dex run [command] [args...]`: bare, it lists the
// project's registered commands; with a command name it evaluates the
// project and runs it with no eval timeout, cancelling on SIGINT.
func newRunCmd(dir *string, d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "run [command] [args...]",
		Short: "Run a custom command",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := d.newRunner(*dir)
			if err != nil {
				return err
			}
			// SIGINT cancels the command's ctx (killing any exec'd child)
			// instead of killing dex mid-evaluation.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if len(args) == 0 {
				commands, listErr := r.List(ctx)
				if listErr != nil {
					return listErr
				}
				for _, c := range commands {
					if c.Description == "" {
						fmt.Fprintln(cmd.OutOrStdout(), c.Name)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", c.Name, c.Description)
					}
				}
				return nil
			}

			code, runErr := r.Run(ctx, args[0], args[1:])
			if runErr != nil {
				return runErr
			}
			if code != 0 {
				return &exitCodeError{code: code}
			}
			return nil
		},
	}
}
