// Package cli implements the dex Cobra commands: init, plan, apply.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

// deps lets tests inject a fake engine factory without booting esbuild/goja.
type deps struct {
	newEngine func(dir string) (*engine.Engine, error)
}

// Main builds the CLI, executes it against args, and returns a process exit
// code. Commands write only through in/out/errOut — never direct os.Stdin
// or fmt.Println — so every path is assertable.
func Main(args []string, in io.Reader, out, errOut io.Writer) int {
	root := newRootCmd(productionDeps())
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)

	if err := root.Execute(); err != nil {
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
	return root
}

func productionDeps() deps {
	return deps{newEngine: newProductionEngine}
}

// newProductionEngine wires the real esbuild/goja synthesizer and on-disk
// state store. Process env only: .env parsing is Phase 2 secrets work.
func newProductionEngine(dir string) (*engine.Engine, error) {
	synth := jsenv.NewSynthesizer(jsenv.NewEsbuildBundler(), jsenv.NewGojaEvaluator())
	return engine.New(engine.Options{
		Synth:    synth,
		Store:    state.NewFileStore(dir, time.Now),
		Lock:     state.NewLock(dir),
		Registry: engine.NewRegistry(),
		Env:      envMap(),
		Out:      os.Stdout,
	}), nil
}

func envMap() map[string]string {
	env := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// entrypointPath returns the dex.config.ts path for a project root,
// erroring with an actionable message when it is missing rather than
// letting esbuild produce an opaque resolve failure.
func entrypointPath(dir string) (string, error) {
	path := filepath.Join(dir, "dex.config.ts")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("no dex.config.ts in %s (run `dex init`)", dir)
	}
	return path, nil
}
