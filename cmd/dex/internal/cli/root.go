// Package cli implements the dex Cobra commands: init, plan, apply.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/freality/dexter/adapters/docker"
	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

// deps lets tests inject a fake engine factory without booting esbuild/goja.
type deps struct {
	newEngine func(dir string) (*engine.Engine, error)
	newRunner func(dir string) (runner, error)
}

// Main builds the CLI, executes it against args, and returns a process exit
// code. Commands write only through in/out/errOut — never direct os.Stdin
// or fmt.Println — so every path is assertable.
func Main(args []string, in io.Reader, out, errOut io.Writer) int {
	return mainWith(productionDeps(), args, in, out, errOut)
}

// mainWith is Main with injectable deps for tests.
func mainWith(d deps, args []string, in io.Reader, out, errOut io.Writer) int {
	root := newRootCmd(d)
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)

	if err := root.Execute(); err != nil {
		var codeErr *exitCodeError
		if errors.As(err, &codeErr) {
			return codeErr.code
		}
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd(d deps) *cobra.Command {
	dir := "."
	root := &cobra.Command{
		Use:           "dex",
		Short:         "Homelab infrastructure as code",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&dir, "dir", ".", "project root directory")
	root.AddCommand(newInitCmd(&dir))
	root.AddCommand(newPlanCmd(&dir, d))
	root.AddCommand(newApplyCmd(&dir, d))
	root.AddCommand(newRunCmd(&dir, d))
	return root
}

func productionDeps() deps {
	return deps{newEngine: newProductionEngine, newRunner: newRunner}
}

// newProductionEngine wires the real esbuild/goja synthesizer, on-disk state
// store, and Docker adapter. .env values are loaded first, then process
// env is overlaid (process env wins for same key).
func newProductionEngine(dir string) (*engine.Engine, error) {
	dock, err := docker.New()
	if err != nil {
		return nil, err
	}
	synth := jsenv.NewSynthesizer(jsenv.NewEsbuildBundler(), jsenv.NewGojaEvaluator())

	env, err := loadEnv(dir)
	if err != nil {
		return nil, err
	}
	mergeProcessEnv(env)

	return engine.New(engine.Options{
		Synth:    synth,
		Store:    state.NewFileStore(dir, time.Now),
		Lock:     state.NewLock(dir),
		Registry: registryFor(dock),
		Env:      env,
		Out:      os.Stdout,
	}), nil
}
