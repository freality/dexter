package cli

import (
	"fmt"
	"os"
	"strings"
)

// loadEnv reads <dir>/.env and returns a map of key-value pairs.
//
// Syntax (one level only, no interpolation or continuation):
//   - KEY=VALUE lines
//   - #-prefixed comment lines (entire line, not inline)
//   - Optional "export " prefix before KEY (trimmed)
//   - One level of matching "…" or '…' quotes stripped from VALUE
//
// Missing .env file returns empty map with nil error.
// Malformed lines (no '=') return an error with the line number.
func loadEnv(dir string) (map[string]string, error) {
	data, err := os.ReadFile(dir + "/.env")
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	env := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf(".env:%d: malformed line (missing '=')", i+1)
		}

		// One level of matching single or double quotes stripped from VALUE
		if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
			v = v[1 : len(v)-1]
		}

		env[k] = v
	}

	return env, nil
}
