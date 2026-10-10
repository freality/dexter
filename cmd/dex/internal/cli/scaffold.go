package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const configTemplate = `import { defineDexConfig, env, service } from "@dex/iac";

defineDexConfig({ project: "homelab" });

service("whoami", {
  plugin: "docker",
  properties: {
    image: "traefik/whoami:latest",
    ports: ["8080:80"],
    env: { TZ: env("TZ", "UTC") },
  },
});
`

const envTemplate = "TZ=UTC\n"

const gitignoreDexLine = ".dex/"

// runInit scaffolds a new project into dir, refusing to clobber an existing
// dex.config.ts (SPEC §3 layout).
func runInit(w io.Writer, dir string) error {
	configPath := filepath.Join(dir, "dex.config.ts")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("dex.config.ts already exists; refusing to overwrite")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check %s: %w", configPath, err)
	}

	if err := writeScaffoldFile(configPath, configTemplate); err != nil {
		return err
	}
	fmt.Fprintln(w, "created dex.config.ts")

	envPath := filepath.Join(dir, ".env")
	if err := writeScaffoldFile(envPath, envTemplate); err != nil {
		return err
	}
	fmt.Fprintln(w, "created .env")

	gitkeepPath := filepath.Join(dir, "dex", "services", ".gitkeep")
	if err := os.MkdirAll(filepath.Dir(gitkeepPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(gitkeepPath), err)
	}
	if err := writeScaffoldFile(gitkeepPath, ""); err != nil {
		return err
	}
	fmt.Fprintln(w, "created dex/services/.gitkeep")

	if err := appendGitignore(dir); err != nil {
		return err
	}
	fmt.Fprintln(w, "created .gitignore")

	return nil
}

func writeScaffoldFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// appendGitignore ensures ".dex/" is ignored, creating .gitignore if
// missing and never duplicating the line on repeated runs.
func appendGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == gitignoreDexLine {
			return nil
		}
	}

	content := string(data)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += gitignoreDexLine + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
