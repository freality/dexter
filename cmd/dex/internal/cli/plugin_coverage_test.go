package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/state"
)

// writeProject writes a minimal project (dex.config.ts content only) to a
// fresh temp dir and returns its path.
func writeProject(t *testing.T, configTS string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte(configTS), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// --- newPluginCmd: addCmd success path --------------------------------------

// TestPluginAddCmdSuccess drives "dex plugin add" through the real cobra
// command end to end, covering addCmd's RunE success return (today only
// installSpec itself, not the command wiring around it, was exercised).
func TestPluginAddCmdSuccess(t *testing.T) {
	name := "dex-adapter-docker_" + assetOS() + "_" + assetArch()
	handler := taggedHandler("v1.0.0", []string{name}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	oldBase := installBaseURL
	installBaseURL = srv.URL
	defer func() { installBaseURL = oldBase }()

	dir := t.TempDir()
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plugin", "add", "--dir", dir, "github.com/owner/repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "installed "+name) {
		t.Fatalf("out = %q", out.String())
	}
}

// --- runPluginInstall: no-plugins-declared and all-installed-OK branches ---

// TestRunPluginInstallNoPluginsDeclared verifies the early return when
// IR.Plugins is empty (a project with no plugins: [...] in dex.config.ts).
func TestRunPluginInstallNoPluginsDeclared(t *testing.T) {
	dir := writeProject(t, `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });
`)
	var out bytes.Buffer
	err := runPluginInstall(dir, fakeEngineDeps(nil, state.New()), &out)
	if err != nil {
		t.Fatalf("runPluginInstall() error = %v", err)
	}
	if !strings.Contains(out.String(), "No plugins declared in dex.config.ts.") {
		t.Fatalf("out = %q", out.String())
	}
}

// TestRunPluginInstallAllSucceed verifies runPluginInstall's final success
// return when every declared plugin spec installs cleanly.
func TestRunPluginInstallAllSucceed(t *testing.T) {
	name := "dex-adapter-docker_" + assetOS() + "_" + assetArch()
	handler := taggedHandler("v1.0.0", []string{name}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	oldBase := installBaseURL
	installBaseURL = srv.URL
	defer func() { installBaseURL = oldBase }()

	dir := writeProject(t, `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p", plugins: ["github.com/owner/repo@v1.0.0"] });
`)
	var out bytes.Buffer
	err := runPluginInstall(dir, fakeEngineDeps(nil, state.New()), &out)
	if err != nil {
		t.Fatalf("runPluginInstall() error = %v", err)
	}
	if !strings.Contains(out.String(), "installed "+name+" v1.0.0") {
		t.Fatalf("out = %q", out.String())
	}
}

// --- synthesizeDoc: synth and parse-IR error branches -----------------------

// TestSynthesizeDocLoadEnvError verifies a loadEnv failure (here: .env is a
// directory, so os.ReadFile fails with something other than not-exist)
// propagates unwrapped from synthesizeDoc.
func TestSynthesizeDocLoadEnvError(t *testing.T) {
	dir := writeProject(t, `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "p" });
`)
	if err := os.Mkdir(filepath.Join(dir, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := synthesizeDoc(nil, dir)
	if err == nil {
		t.Fatal("synthesizeDoc() error = nil, want a loadEnv error")
	}
}

// TestSynthesizeDocSynthError verifies a synth-time failure (here: the SDK's
// own eager INVALID_PROJECT check) is wrapped as a "synthesize" error.
func TestSynthesizeDocSynthError(t *testing.T) {
	dir := writeProject(t, `import { defineDexConfig } from "@dex/iac";
defineDexConfig({ project: "" });
`)
	_, err := synthesizeDoc(nil, dir)
	if err == nil || !strings.Contains(err.Error(), "synthesize:") {
		t.Fatalf("err = %v, want a wrapped synthesize error", err)
	}
}

// TestSynthesizeDocParseIRError verifies a script that bypasses the SDK and
// returns a malformed JSON string from __dex_synth directly (legitimate for
// any script that doesn't use @dex/iac's defineDexConfig, or a future SDK
// version skewed from this Go build) is wrapped as a "parse IR" error.
func TestSynthesizeDocParseIRError(t *testing.T) {
	dir := writeProject(t, `(globalThis as any).__dex_synth = () => "not valid json";
`)
	_, err := synthesizeDoc(nil, dir)
	if err == nil || !strings.Contains(err.Error(), "parse IR:") {
		t.Fatalf("err = %v, want a wrapped parse IR error", err)
	}
}

// --- installSpec: checksums.txt download error ------------------------------

// TestInstallSpecChecksumsDownloadError verifies a checksums.txt asset whose
// URL 404s is wrapped as a "download checksums.txt" error.
func TestInstallSpecChecksumsDownloadError(t *testing.T) {
	var checksumsURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/repos/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"checksums.txt","browser_download_url":"` + checksumsURL + `"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	checksumsURL = srv.URL + "/missing-checksums"

	var out bytes.Buffer
	err := installSpec(t.TempDir(), "github.com/owner/repo", srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "download checksums.txt:") {
		t.Fatalf("err = %v, want a wrapped download checksums.txt error", err)
	}
}

// --- fetchRelease: response body read error ---------------------------------

// TestFetchReleaseBodyReadError verifies a response that promises more body
// bytes than it actually sends (Content-Length lies, connection closes
// early) is wrapped as a "read release response" error.
func TestFetchReleaseBodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{\"tag")
		buf.Flush()
	}))
	defer srv.Close()

	_, err := fetchRelease(srv.URL, "owner", "repo", "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "read release response:") {
		t.Fatalf("err = %v, want a wrapped read release response error", err)
	}
}
