package jsenv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeProject writes files (relative slash paths -> contents) under dir.
func writeProject(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscover(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		mutate  func(t *testing.T, dir string)
		want    []string
		wantErr string // substring of the error, empty for success
	}{
		{
			name:  "config only",
			files: map[string]string{"dex.config.ts": "export {};"},
			want:  []string{"dex.config.ts"},
		},
		{
			name: "config plus services",
			files: map[string]string{
				"dex.config.ts":           "export {};",
				"dex/services/b.ts":       "export {};",
				"dex/services/a.ts":       "export {};",
				"dex/services/ignore.txt": "not ts",
			},
			want: []string{"dex.config.ts", "dex/services/a.ts", "dex/services/b.ts"},
		},
		{
			name: "nested subdirectories",
			files: map[string]string{
				"dex.config.ts":       "export {};",
				"dex/services/a.ts":   "export {};",
				"dex/services/a/b.ts": "export {};",
			},
			want: []string{"dex.config.ts", "dex/services/a.ts", "dex/services/a/b.ts"},
		},
		{
			name: "d.ts files skipped",
			files: map[string]string{
				"dex.config.ts":              "export {};",
				"dex/services/a.ts":          "export {};",
				"dex/services/types.d.ts":    "export type Foo = string;",
				"dex/services/nested/x.d.ts": "export type Bar = number;",
			},
			want: []string{"dex.config.ts", "dex/services/a.ts"},
		},
		{
			name: "dot-prefixed directories skipped",
			files: map[string]string{
				"dex.config.ts":              "export {};",
				"dex/services/a.ts":          "export {};",
				"dex/services/.git/hook.ts":  "export {};",
				"dex/services/.dex/state.ts": "export {};",
			},
			want: []string{"dex.config.ts", "dex/services/a.ts"},
		},
		{
			name:  "missing services dir",
			files: map[string]string{"dex.config.ts": "export {};"},
			want:  []string{"dex.config.ts"},
		},
		{
			name:    "missing config",
			files:   map[string]string{"dex/services/a.ts": "export {};"},
			wantErr: "no dex.config.ts in",
		},
		{
			name:  "services stat error propagates",
			files: map[string]string{"dex.config.ts": "export {};", "dex": ""},
			// "dex" is a file, so statting dex/services fails with ENOTDIR,
			// which is not IsNotExist and must not be swallowed.
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "dex")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "dex"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "check",
		},
		{
			name:  "walk error propagates",
			files: map[string]string{"dex.config.ts": "export {};", "dex/services/a.ts": "export {};"},
			// An unreadable subdirectory makes WalkDir fail; the error must
			// surface rather than silently returning a partial file list.
			mutate: func(t *testing.T, dir string) {
				locked := filepath.Join(dir, "dex", "services", "locked")
				if err := os.MkdirAll(locked, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(locked, "b.ts"), []byte(""), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(locked, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(locked, 0o755) })
			},
			wantErr: "walk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Geteuid() == 0 && (tt.name == "walk error propagates") {
				t.Skip("root ignores directory permissions; skipping")
			}
			dir := t.TempDir()
			writeProject(t, dir, tt.files)
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}

			got, err := Discover(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Discover() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Discover() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSynthOverTempDir drives the real esbuild + goja + embedded SDK pipeline
// over t.TempDir() projects, so the discovered files genuinely bundle into
// one shared-registry IR.
func TestSynthOverTempDir(t *testing.T) {
	t.Run("single dex.config.ts only", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig, service } from "@dex/iac";
defineDexConfig({ project: "p" });
service("whoami", { plugin: "docker", properties: { image: "traefik/whoami" } });`,
		})
		assertResources(t, dir, map[string]string{"whoami": "docker"})
	})

	t.Run("services files bundle into one IR", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
			"dex/services/a.ts": `import { service } from "@dex/iac";
service("a", { plugin: "docker", properties: {} });`,
			"dex/services/nested/b.ts": `import { service } from "@dex/iac";
service("b", { plugin: "docker", properties: {} });`,
		})
		assertResources(t, dir, map[string]string{"a": "docker", "b": "docker"})
	})

	t.Run("cross-file import shares the registry", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
			"dex/services/net.ts": `import { network } from "@dex/iac";
export const net = network("net", { plugin: "docker", properties: {} });`,
			"dex/services/app.ts": `import { service } from "@dex/iac";
import { net } from "./net";
service("app", { plugin: "docker", properties: {}, dependsOn: [net.name] });`,
		})
		got := synthResources(t, dir)
		app := got["app"]
		if app.DependsOn == nil || !reflect.DeepEqual(app.DependsOn, []string{"net"}) {
			t.Fatalf("app depends_on = %v, want [net]", app.DependsOn)
		}
		if _, ok := got["net"]; !ok {
			t.Fatalf("IR missing net: %v", got)
		}
	})

	t.Run("d.ts file ignored", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
			"dex/services/a.ts": `import { service } from "@dex/iac";
service("a", { plugin: "docker", properties: {} });`,
			// TypeScript that would fail the bundle if it were ever imported.
			"dex/services/broken.d.ts": "this is not type syntax {{{",
		})
		assertResources(t, dir, map[string]string{"a": "docker"})
	})

	t.Run("missing services dir still synthesizes", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
		})
		got := synthResources(t, dir)
		if len(got) != 0 {
			t.Fatalf("resources = %v, want none", got)
		}
	})

	t.Run("duplicate resource across files", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
			"dex/services/one.ts": `import { service } from "@dex/iac";
service("dup", { plugin: "docker", properties: {} });`,
			"dex/services/two.ts": `import { service } from "@dex/iac";
service("dup", { plugin: "docker", properties: {} });`,
		})
		s := NewSynthesizer(NewEsbuildBundler(), NewGojaEvaluator())
		_, err := s.Synth(context.Background(), dir, nil)
		if err == nil || !strings.Contains(err.Error(), "[DUPLICATE_RESOURCE]") ||
			!strings.Contains(err.Error(), `resource "dup" is already defined`) {
			t.Fatalf("Synth() error = %v, want [DUPLICATE_RESOURCE]", err)
		}
	})

	t.Run("syntax error reports the file and line", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, map[string]string{
			"dex.config.ts": `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });`,
			"dex/services/bad.ts": "import { service } from \"@dex/iac\";\nservice(\"bad\", { plugin: \"docker\", properties: { } );\n",
		})
		s := NewSynthesizer(NewEsbuildBundler(), NewGojaEvaluator())
		_, err := s.Synth(context.Background(), dir, nil)
		if err == nil || !strings.Contains(err.Error(), "bad.ts") {
			t.Fatalf("Synth() error = %v, want the offending file named", err)
		}
		if err != nil && !strings.Contains(err.Error(), ":2:") {
			t.Fatalf("Synth() error = %v, want line 2", err)
		}
	})
}

type testResource struct {
	Type      string   `json:"type"`
	Plugin    string   `json:"plugin"`
	DependsOn []string `json:"depends_on"`
}

func synthResources(t *testing.T, dir string) map[string]testResource {
	t.Helper()
	s := NewSynthesizer(NewEsbuildBundler(), NewGojaEvaluator())
	data, err := s.Synth(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("Synth() error = %v", err)
	}
	var ir struct {
		Resources map[string]testResource `json:"resources"`
	}
	if err := json.Unmarshal(data, &ir); err != nil {
		t.Fatalf("unmarshal IR %s: %v", data, err)
	}
	return ir.Resources
}

func assertResources(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	got := synthResources(t, dir)
	for name, plugin := range want {
		res, ok := got[name]
		if !ok {
			t.Fatalf("IR missing resource %q (have %v)", name, got)
		}
		if res.Plugin != plugin {
			t.Fatalf("%s plugin = %q, want %q", name, res.Plugin, plugin)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
}
