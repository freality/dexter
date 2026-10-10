// Command gen bundles packages/sdk/dist (tsc output) into a single ESM file
// embedded into the dex binary as pkg/jsenv/sdk/iac.js. The user has no
// Node.js and no node_modules, so @dex/iac must ship inside the binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
)

// Run locates the repo root, bundles packages/sdk/dist/index.js, and writes
// pkg/jsenv/sdk/iac.js.
func Run() error {
	root, err := findRepoRoot()
	if err != nil {
		return fmt.Errorf("generate SDK bundle: %w", err)
	}
	entry := filepath.Join(root, "packages", "sdk", "dist", "index.js")
	if _, err := os.Stat(entry); err != nil {
		return fmt.Errorf("generate SDK bundle: %w", err)
	}

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatESModule,
		Platform:    api.PlatformNeutral,
		Target:      api.ES2017,
	})
	if len(result.Errors) > 0 {
		msg := result.Errors[0].Text
		if loc := result.Errors[0].Location; loc != nil {
			msg = fmt.Sprintf("%s:%d: %s", loc.File, loc.Line, msg)
		}
		return fmt.Errorf("generate SDK bundle: %s", msg)
	}

	out := filepath.Join(root, "pkg", "jsenv", "sdk", "iac.js")
	if err := os.WriteFile(out, result.OutputFiles[0].Contents, 0o644); err != nil {
		return fmt.Errorf("generate SDK bundle: %w", err)
	}
	return nil
}

// findRepoRoot walks up from the working directory to the directory
// containing go.work.
func findRepoRoot() (string, error) {
	// os.Getwd failing (cwd deleted mid-run) is unreachable in practice; a
	// stale/empty path just falls through the walk-up loop below and fails
	// the same way a missing go.work does.
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.work not found above %s", dir)
		}
		dir = parent
	}
}

// Main runs the generator and returns a process exit code, printing any
// error to stderr.
func Main() int {
	if err := Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
