package jsenv

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Discover returns the project's TypeScript entry files in bundle order:
// dex.config.ts first, then every dex/services/**/*.ts below dir as
// slash-separated paths relative to dir, sorted. dex.config.ts must exist —
// its absence is the "not a dex project" error, reported here rather than as
// an opaque esbuild resolve failure. A missing dex/services/ directory is
// fine (contributes nothing), and *.d.ts files and dot-prefixed directories
// (e.g. .git, .dex) are skipped. The registry and buildIR sort internally,
// so file order only makes the first error deterministic.
func Discover(dir string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(dir, "dex.config.ts")); err != nil {
		return nil, fmt.Errorf("no dex.config.ts in %s (run `dex init`)", dir)
	}
	files := []string{"dex.config.ts"}

	services := filepath.Join(dir, "dex", "services")
	if _, err := os.Stat(services); err != nil {
		if os.IsNotExist(err) {
			return files, nil
		}
		return nil, fmt.Errorf("check %s: %w", services, err)
	}
	err := filepath.WalkDir(services, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".d.ts") {
			return nil
		}
		// path always starts with services (the walk root), so the slash path
		// relative to dir is that prefix plus the tail; a Rel call could not
		// fail here, so there is no error branch to keep covered.
		files = append(files, "dex/services"+filepath.ToSlash(path[len(services):]))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", services, err)
	}

	sort.Strings(files[1:])
	return files, nil
}
