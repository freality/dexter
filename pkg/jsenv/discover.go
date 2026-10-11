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
// dex.config.ts first, then every dex/services/**/*.ts and dex/commands/**/*.ts
// below dir as slash-separated paths relative to dir, sorted. dex.config.ts
// must exist — its absence is the "not a dex project" error, reported here
// rather than as an opaque esbuild resolve failure. A missing
// dex/services/ or dex/commands/ directory is fine (contributes nothing),
// and *.d.ts files and dot-prefixed directories (e.g. .git, .dex) are
// skipped. The registry and buildIR sort internally, so file order only
// makes the first error deterministic.
func Discover(dir string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(dir, "dex.config.ts")); err != nil {
		return nil, fmt.Errorf("no dex.config.ts in %s (run `dex init`)", dir)
	}
	files := []string{"dex.config.ts"}

	// Commands (D13) live anywhere Discover looks; services and commands
	// share the bundle and the SDK singletons.
	for _, root := range []string{"dex/services", "dex/commands"} {
		walked, err := walkTS(filepath.Join(dir, root), root)
		if err != nil {
			return nil, err
		}
		files = append(files, walked...)
	}

	sort.Strings(files[1:])
	return files, nil
}

// walkTS returns every *.ts file below dir (a missing dir contributes
// nothing), as slash paths relative to the project root and rooted at the
// given slash root ("dex/services" or "dex/commands"). *.d.ts files and
// dot-prefixed directories are skipped.
func walkTS(dir, root string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("check %s: %w", dir, err)
	}
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
		// path always starts with the walk root, so the slash path relative
		// to the project root is root plus the tail; a Rel call could not fail
		// here, so there is no error branch to keep covered.
		files = append(files, root+filepath.ToSlash(path[len(dir):]))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return files, nil
}
