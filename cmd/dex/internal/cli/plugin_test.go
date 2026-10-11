package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// --- helpers ----------------------------------------------------------------

// releaseBody builds a GitHub releases API response body for the given assets.
func releaseBody(baseURL, tag string, assetsName []string) string {
	r := fmt.Sprintf(`{"tag_name":%q,"assets":[`, tag)
	for i, name := range assetsName {
		if i > 0 {
			r += ","
		}
		r += fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/assets/%s","size":1234}`, name, baseURL, name)
	}
	r += "]}"
	return r
}

// checksumsBody builds checksums.txt content for the given assets.
func checksumsBody(assetsName []string) string {
	r := ""
	for _, name := range assetsName {
		data := []byte(name + name + name)
		h := sha256.Sum256(data)
		var hashBuf []byte
		for i := range h {
			hashBuf = append(hashBuf, h[i])
		}
		r += hex.EncodeToString(hashBuf) + "  " + name + "\n"
	}
	return r
}

// requestBase computes scheme://host:port from the request for building absolute URLs.
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// taggedHandler returns an httptest-compatible handler that serves release
// info and binary assets. tag is the release tag, assets are the asset names.
// setToken, if non-nil, is called with the Authorization header value.
func taggedHandler(tag string, assets []string, setToken func(string)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		hdrs := w.Header()
		hdrs.Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "/repos/") || strings.Contains(path, "latest") || strings.Contains(path, "tags/") {
			auth := r.Header.Get("Authorization")
			if setToken != nil {
				setToken(auth)
			}
			w.WriteHeader(200)
			// Include checksums.txt as a release asset so installSpec can find it.
			allAssets := make([]string, len(assets)+1)
			allAssets[0] = "checksums.txt"
			copy(allAssets[1:], assets)
			io.WriteString(w, releaseBody(requestBase(r), tag, allAssets))
			return
		}

		if strings.Contains(path, "/assets/") {
			name := path[len("/assets/"):]
			if strings.HasSuffix(name, "checksums.txt") {
				hdrs.Set("Content-Type", "text/plain")
				w.WriteHeader(200)
				io.WriteString(w, checksumsBody(assets))
				return
			}
			hdrs.Set("Content-Type", "application/octet-stream")
			w.WriteHeader(200)
			io.WriteString(w, name+name+name)
			return
		}

		w.WriteHeader(404)
		io.WriteString(w, "not found")
	}
}

// writeTestPlugin writes an already-installed binary with content name+name+name.
func writeTestPlugin(t *testing.T, dir, name string) {
	pluginsDir := filepath.Join(dir, ".dex", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(name + name + name)
	dest := filepath.Join(pluginsDir, name)
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

// tmpFiles returns .tmp files under dir/.dex/plugins (used for assertions).
func tmpFiles(dir string) []string {
	p := filepath.Join(dir, ".dex", "plugins")
	if _, err := os.Stat(p); err != nil {
		return []string{}
	}
	var result []string
	entries, dirErr := os.ReadDir(p)
	if dirErr != nil {
		return result
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			result = append(result, filepath.Join(p, entry.Name()))
		}
	}
	return result
}

// --- specParts tests --------------------------------------------------------

func TestSpecPartsRepoOnly(t *testing.T) {
	owner, repo, tag, err := specParts("github.com/owner/repo")
	if err != nil || owner != "owner" || repo != "repo" || tag != "" {
		t.Fatalf("specParts = (%q, %q, %q, %v), want (owner, repo, , nil)", owner, repo, tag, err)
	}
}

func TestSpecPartsWithTag(t *testing.T) {
	owner, repo, tag, err := specParts("github.com/freality/dexter@v1.0.0")
	if err != nil || owner != "freality" || repo != "dexter" || tag != "v1.0.0" {
		t.Fatalf("specParts = (%q, %q, %q, %v)", owner, repo, tag, err)
	}
}

func TestSpecPartsLatestTag(t *testing.T) {
	owner, repo, tag, err := specParts("github.com/a/b@latest")
	if err != nil || owner != "a" || repo != "b" || tag != "latest" {
		t.Fatalf("specParts = (%q, %q, %q, %v)", owner, repo, tag, err)
	}
}

func TestSpecPartsInvalid(t *testing.T) {
	_, _, _, err := specParts("not-a-url")
	if err == nil || !strings.Contains(err.Error(), "invalid plugin spec") {
		t.Fatalf("err = %v, want invalid plugin spec", err)
	}
}

// --- parseChecksums tests ---------------------------------------------------

func TestParseChecksumsHappyPath(t *testing.T) {
	c := parseChecksums("abc123  file1\n  def456  file2  \n# comment\n")
	if len(c) != 2 || !strings.HasPrefix(c["file1"], "abc") || !strings.HasPrefix(c["file2"], "def") {
		t.Fatalf("checksums = %+v", c)
	}
}

func TestParseChecksumsSkipsShortLines(t *testing.T) {
	c := parseChecksums("abc\nabc123  file1\n")
	if len(c) != 1 {
		t.Fatalf("checksums = %+v, want 1 entry", c)
	}
}

func TestParseChecksumsSkipsNonHex(t *testing.T) {
	c := parseChecksums("xyz123  bad\nabc123  file1\n")
	if len(c) != 1 {
		t.Fatalf("checksums = %+v, want 1 entry", c)
	}
}

func TestIsHexString(t *testing.T) {
	if !isHexString("abc123") {
		t.Fatal("isHexString(abc123) = false")
	}
	if isHexString("xyz") {
		t.Fatal("isHexString(xyz) = true")
	}
	if isHexString("") {
		t.Fatal("isHexString('') = true")
	}
}

// --- installSpec tests with httptest.Server ---------------------------------

func TestPluginAddLatest(t *testing.T) {
	dir := t.TempDir()
	handler := taggedHandler("v1.0.0", []string{"dex-adapter-docker_darwin_arm64"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err != nil {
		t.Fatalf("installSpec error = %v", err)
	}
	if !strings.Contains(out.String(), "installed dex-adapter-docker_darwin_arm64 v1.0.0") {
		t.Fatalf("out = %q", out.String())
	}
	_, statErr := os.Stat(filepath.Join(dir, ".dex", "plugins", "dex-adapter-docker_darwin_arm64"))
	if statErr != nil {
		t.Fatalf("binary not installed: %v", statErr)
	}
}

func TestPluginAddTagged(t *testing.T) {
	dir := t.TempDir()
	handler := taggedHandler("v2.0.0", []string{"dex-adapter-docker_darwin_arm64"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo@v2.0.0", srv.URL, &out)
	if err != nil {
		t.Fatalf("installSpec error = %v", err)
	}
	if !strings.Contains(out.String(), "installed dex-adapter-docker_darwin_arm64 v2.0.0") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestPluginAddMultipleAdapters(t *testing.T) {
	dir := t.TempDir()
	handler := taggedHandler("v1.0.0", []string{
		"dex-adapter-docker_darwin_arm64",
		"dex-adapter-proxmox_darwin_arm64",
	}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err != nil {
		t.Fatalf("installSpec error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "installed dex-adapter-docker_darwin_arm64") {
		t.Fatalf("missing docker adapter: %q", output)
	}
	if !strings.Contains(output, "installed dex-adapter-proxmox_darwin_arm64") {
		t.Fatalf("missing proxmox adapter: %q", output)
	}
}

func TestPluginAddChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	handler := func(w http.ResponseWriter, r *http.Request) {
		hdrs := w.Header()
		hdrs.Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.Contains(path, "/repos/") || strings.Contains(path, "latest") || strings.Contains(path, "tags/") {
			w.WriteHeader(200)
			// Include checksums.txt so installSpec can find and verify it.
			io.WriteString(w, releaseBody(requestBase(r), "v1.0.0", []string{"checksums.txt", "dex-adapter-docker_darwin_arm64"}))
			return
		}
		if strings.Contains(path, "/assets/") {
			name := path[len("/assets/"):]
			if strings.HasSuffix(name, "checksums.txt") {
				hdrs.Set("Content-Type", "text/plain")
				w.WriteHeader(200)
				io.WriteString(w, "0000000000000000000000000000000000000000000000000000000000000000  dex-adapter-docker_darwin_arm64\n")
				return
			}
			hdrs.Set("Content-Type", "application/octet-stream")
			w.WriteHeader(200)
			io.WriteString(w, name) // different from name+name+name
			return
		}
		w.WriteHeader(404)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch error", err)
	}
	if leftover := tmpFiles(dir); len(leftover) > 0 {
		t.Fatalf("leftover .tmp file: %v", leftover)
	}
}

func TestPluginAddNoMatchingAsset(t *testing.T) {
	dir := t.TempDir()
	handler := taggedHandler("v1.0.0", []string{"dex-adapter-docker_windows_amd64"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "no adapter asset") {
		t.Fatalf("err = %v, want no-adapter-asset error", err)
	}
}

func TestPluginAddTokenHeader(t *testing.T) {
	dir := t.TempDir()
	var capturedToken string
	handler := taggedHandler("v1.0.0", []string{"dex-adapter-docker_darwin_arm64"}, func(s string) { capturedToken = s })
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "ghp_test123")

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err != nil {
		t.Fatalf("installSpec error = %v", err)
	}
	if !strings.Contains(capturedToken, "Bearer ghp_test123") {
		t.Fatalf("Authorization header = %q, want Bearer ghp_test123", capturedToken)
	}
}

func TestPluginAddAlreadySatisfied(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "dex-adapter-docker_darwin_arm64")

	handler := taggedHandler("v1.0.0", []string{"dex-adapter-docker_darwin_arm64"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err != nil {
		t.Fatalf("installSpec error = %v", err)
	}
	if !strings.Contains(out.String(), "already installed") {
		t.Fatalf("out = %q, want already-installed message", out.String())
	}
}

func TestPluginAddNoChecksums(t *testing.T) {
	dir := t.TempDir()
	handler := func(w http.ResponseWriter, r *http.Request) {
		hdrs := w.Header()
		hdrs.Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.Contains(path, "/repos/") || strings.Contains(path, "latest") || strings.Contains(path, "tags/") {
			w.WriteHeader(200)
			io.WriteString(w, releaseBody(requestBase(r), "v1.0.0", []string{"dex-adapter-docker_darwin_arm64"}))
			return
		}
		w.WriteHeader(404)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "no checksums.txt") {
		t.Fatalf("err = %v, want no-checksums error", err)
	}
}

// --- isAlreadyInstalled tests -----------------------------------------------

func TestIsAlreadyInstalledMissing(t *testing.T) {
	if isAlreadyInstalled("/nonexistent/path", "abc") {
		t.Fatal("isAlreadyInstalled on missing file returned true")
	}
}

func TestIsAlreadyInstalledMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := []byte("hello")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	var buf []byte
	for i := range h {
		buf = append(buf, h[i])
	}
	hash := hex.EncodeToString(buf)
	if !isAlreadyInstalled(path, hash) {
		t.Fatal("isAlreadyInstalled returned false for matching file")
	}
}

func TestIsAlreadyInstalledMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isAlreadyInstalled(path, "0000") {
		t.Fatal("isAlreadyInstalled returned true for mismatching hash")
	}
}

// --- assetOS/arch tests -----------------------------------------------------

func TestAssetOSandArch(t *testing.T) {
	if assetOS() == "" {
		t.Fatal("assetOS() returned empty string")
	}
	if assetArch() == "" {
		t.Fatal("assetArch() returned empty string")
	}
}

// --- downloadAndVerify tests -------------------------------------------------

func TestDownloadAndVerifySuccess(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello")
	h := sha256.Sum256(data)
	var buf []byte
	for i := range h {
		buf = append(buf, h[i])
	}
	hash := hex.EncodeToString(buf)

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(data)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := filepath.Join(dir, "result.bin")
	err := downloadAndVerify(srv.URL+"/data", dest, hash)
	if err != nil {
		t.Fatalf("downloadAndVerify error = %v", err)
	}
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Fatal(".tmp file still exists")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "hello" {
		t.Fatalf("content = %q, want hello", string(got))
	}
}

func TestDownloadAndVerifyChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "hello")
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	dest := filepath.Join(dir, "result.bin")
	err := downloadAndVerify(srv.URL+"/data", dest, "0000")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Fatal(".tmp file survived after checksum mismatch")
	}
}

// --- assetURL ----------------------------------------------------------------

func TestAssetURLFound(t *testing.T) {
	ri := &releaseInfo{TagName: "v1", Assets: []releaseAsset{
		{Name: "checksums.txt", URL: "http://example.com/checksums"},
		{Name: "adapter.bin", URL: "http://example.com/adapter"},
	}}
	url := ri.assetURL("checksums.txt")
	if url != "http://example.com/checksums" {
		t.Fatalf("assetURL = %q", url)
	}
}

func TestAssetURLNotFound(t *testing.T) {
	ri := &releaseInfo{TagName: "v1"}
	url := ri.assetURL("nonexistent.txt")
	if url != "" {
		t.Fatalf("assetURL = %q, want empty", url)
	}
}

// --- fetchRelease -----------------------------------------------------------

func TestFetchRelease(t *testing.T) {
	handler := taggedHandler("v3.0.0", []string{"dex-adapter-foo_darwin_arm64"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	ri, err := fetchRelease(srv.URL, "owner", "repo", "")
	if err != nil {
		t.Fatalf("fetchRelease error = %v", err)
	}
	if ri.TagName != "v3.0.0" {
		t.Fatalf("TagName = %q, want v3.0.0", ri.TagName)
	}
	// taggedHandler adds checksums.txt + the test asset.
	if len(ri.Assets) < 1 {
		t.Fatalf("Assets = %v, want at least 1 asset", ri.Assets)
	}
	found := false
	for _, a := range ri.Assets {
		if a.Name == "dex-adapter-foo_darwin_arm64" && strings.HasPrefix(a.URL, srv.URL) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected asset dex-adapter-foo_darwin_arm64 not found in release")
	}
}

// --- downloadBytes ----------------------------------------------------------

func TestDownloadBytes(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "hello world")
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	data, err := downloadBytes(srv.URL + "/test")
	if err != nil {
		t.Fatalf("downloadBytes error = %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("data = %q, want hello world", string(data))
	}
}

func TestDownloadBytes404(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	_, err := downloadBytes(srv.URL + "/missing")
	if err == nil {
		t.Fatal("downloadBytes on 404 returned nil error")
	}
}

// --- newPluginCmd tests (cli command invocation) -----------------------------

func TestPluginAddNoArgs(t *testing.T) {
	dir := t.TempDir()
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"plugin", "add", "--dir", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for add with no args")
	}
}

func TestPluginInstallNoPlugins(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	// Override installBaseURL to a URL that fails fast so the test doesn't hang.
	// Override installBaseURL so the install loop fails instead of making network calls.
	oldBase := installBaseURL
	installBaseURL = "http://127.0.0.1:1/"
	defer func() { installBaseURL = oldBase }()

	// mustRunInit creates a project with plugins, so runPluginInstall will attempt to install.
	cmd := newRootCmd(productionDeps())
	cmd.SetArgs([]string{"plugin", "install", "--dir", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Expected install error (network failure), got nil")
	}
}

// --- assetOS / assetArch coverage -------------------------------------------

func TestAssetOSDarwin(t *testing.T) {
	gos := assetOS()
	if gos != "darwin" && gos != "linux" && gos != "windows" {
		t.Fatalf("assetOS() = %q, unexpected value", gos)
	}
	// We accept any valid OS; this just calls the function for coverage.
}

func TestAssetArchARM64(t *testing.T) {
	ga := assetArch()
	if ga != "amd64" && ga != "arm64" {
		t.Fatalf("assetArch() = %q, unexpected value", ga)
	}
	// Same as above: ensure the function is called.
}

// --- isAlreadyInstalled edge cases ------------------------------------------

func TestIsAlreadyInstalledStatError(t *testing.T) {
	// Missing file already tested in TestIsAlreadyInstalledMissing.
	// Test a permission-denied path (dir instead of file).
	dir := t.TempDir()
	if isAlreadyInstalled(dir, "hash") {
		t.Fatal("isAlreadyInstalled on directory returned true")
	}
}

// --- downloadAndVerify edge cases ------------------------------------------

func TestDownloadAndVerifyDownloadError(t *testing.T) {
	dir := t.TempDir()
	err := downloadAndVerify("http://127.0.0.1:1/does-not-exist", filepath.Join(dir, "out"), "hash")
	if err == nil {
		t.Fatal("downloadAndVerify with bad URL should fail")
	}
}

// --- fetchRelease error paths ------------------------------------------------

func TestFetchRelease404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	_, err := fetchRelease(srv.URL, "owner", "repo", "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "release not found") {
		t.Fatalf("err = %v, want 'release not found'", err)
	}
}

func TestFetchReleaseNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	_, err := fetchRelease(srv.URL, "owner", "repo", "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want HTTP 500 error", err)
	}
}

func TestFetchReleaseBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, "not json")
	}))
	defer srv.Close()

	_, err := fetchRelease(srv.URL, "owner", "repo", "")
	if err == nil || !strings.Contains(err.Error(), "parse release JSON") {
		t.Fatalf("err = %v, want 'parse release JSON' error", err)
	}
}

func TestFetchRelease403Retry(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.WriteHeader(403)
			io.WriteString(w, `{"message":"rate limited"}`)
			return
		}
		w.WriteHeader(200)
		io.WriteString(w, `{"tag_name":"v1","assets":[]}`)
	}))
	defer srv.Close()

	ri, err := fetchRelease(srv.URL, "owner", "repo", "")
	if err != nil {
		t.Fatalf("fetchRelease error = %v (expected retry to succeed)", err)
	}
	if ri.TagName != "v1" {
		t.Fatalf("TagName = %q, want v1", ri.TagName)
	}
	if callCount != 2 {
		t.Fatalf("handler called %d times, want 2 (first 403, then 200)", callCount)
	}
}

func TestFetchRelease403Twice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		io.WriteString(w, `{"message":"rate limited"}`)
	}))
	defer srv.Close()

	_, err := fetchRelease(srv.URL, "owner", "repo", "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want HTTP 403 error after retry", err)
	}
}

// --- downloadBytes error: request creation ---------------------------------

func TestDownloadBytesBadURL(t *testing.T) {
	_, err := downloadBytes("://bad-url")
	if err == nil {
		t.Fatal("downloadBytes with bad URL should fail")
	}
}

// --- fetchRelease request creation error ----------------------------------

func TestFetchReleaseBadBaseURL(t *testing.T) {
	_, err := fetchRelease("://bad", "owner", "repo", "")
	if err == nil {
		t.Fatal("fetchRelease with bad baseURL should fail")
	}
}

// --- newPluginCmd remaining branches -----------------------------------------

func TestPluginAddInstallSpecError(t *testing.T) {
	oldBase := installBaseURL
	installBaseURL = "http://127.0.0.1:1/"
	defer func() { installBaseURL = oldBase }()

	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"plugin", "add", "--dir", t.TempDir(), "github.com/fake/owner@v1.0.0"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Expected installSpec error, got nil")
	}
}

func TestPluginAddInvalidSpec(t *testing.T) {
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"plugin", "add", "--dir", t.TempDir(), "not-a-valid-spec"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid plugin spec") {
		t.Fatalf("Expected invalid plugin spec error, got: %v", err)
	}
}

func TestPluginInstallEngineError(t *testing.T) {
	cmd := newRootCmd(failingEngineDeps(errors.New("engine fail")))
	cmd.SetArgs([]string{"plugin", "install", "--dir", t.TempDir()})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "engine fail") {
		t.Fatalf("Expected engine error, got: %v", err)
	}
}

// installSpec error: mkdir failure
func TestInstallSpecMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Make .dex a file so MkdirAll(".dex/plugins") fails.
	if err := os.WriteFile(filepath.Join(dir, ".dex"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := installSpec(dir, "github.com/owner/repo", "http://127.0.0.1:1/", io.Discard)
	if err == nil {
		t.Fatal("Expected mkdir error, got nil")
	}
}

// downloadAndVerify WriteFile failure (tmpPath is a dir)
func TestDownloadAndVerifyWriteFail(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "test content")
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	h := sha256.Sum256([]byte("test content"))
	var buf []byte
	for i := range h {
		buf = append(buf, h[i])
	}
	expectedHash := hex.EncodeToString(buf)

	dir := t.TempDir()
	// Point dest at a path in a non-existent directory so WriteFile(tmpPath) fails.
	dest := filepath.Join(dir, "nonexistent", "out")
	err := downloadAndVerify(srv.URL+"/data", dest, expectedHash)
	if err == nil {
		t.Fatalf("Expected write error, got nil")
	}
}

// downloadAndVerify Rename failure
func TestDownloadAndVerifyRenameFail(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "renametest")
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	h := sha256.Sum256([]byte("renametest"))
	var buf []byte
	for i := range h {
		buf = append(buf, h[i])
	}
	expectedHash := hex.EncodeToString(buf)

	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	// Create a directory at dest so rename over it fails.
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	err := downloadAndVerify(srv.URL+"/data", dest, expectedHash)
	if err == nil {
		t.Fatal("Expected rename error, got nil")
	}
}

// --- synthesizeDoc tests ----------------------------------------------------

func TestSynthDocMissingConfig(t *testing.T) {
	dir := t.TempDir()
	// No dex.config.ts - synthesizeDoc should error.
	_, err := synthesizeDoc(nil, dir)
	if err == nil {
		t.Fatal("Expected error from synth with missing dex.config.ts")
	}
}

func TestPluginInstallWithPluginsDeclared(t *testing.T) {
	dir := t.TempDir()
	// Create a minimal project that declares plugins.
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte(`import { defineDexConfig } from "@dex/iac";
	defineDexConfig({ project: "p", plugins: ["github.com/owner/repo@v1.0.0"] });
	`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Need dex/services directory for Discover's error message not to fire first.
	// Actually, Discover fails on missing dex.config.ts only, services dir is optional.

	oldBase := installBaseURL
	installBaseURL = "http://127.0.0.1:1/"
	defer func() { installBaseURL = oldBase }()

	// Use the production deps (real engine) so the plugin spec flows through.
	// Note: runPluginInstall creates its own real synth, so deps are used only for the engine
	// (which is needed for the synth call). Use productionDeps for the real engine.
	cmd := newRootCmd(productionDeps())
	cmd.SetArgs([]string{"plugin", "install", "--dir", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Expected install error from network failure, got nil")
	}
}

// --- downloadAndVerify download fail ---------------------------------------

func TestDownloadAndVerifyDownloadFail(t *testing.T) {
	dir := t.TempDir()
	err := downloadAndVerify("http://127.0.0.1:1/blob", filepath.Join(dir, "out"), "abcdef")
	if err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("Expected download error, got: %v", err)
	}
}

// TestRunPluginInstallSynthError: runPluginInstall's error from synthesizeDoc
func TestRunPluginInstallSynthError(t *testing.T) {
	dir := t.TempDir()
	// Write a dex.config.ts with a syntax error.
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte(`const x = ;`), 0644); err != nil {
		t.Fatal(err)
	}
	err := runPluginInstall(dir, productionDeps(), io.Discard)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

// TestSynthesizeDocParseError: ir.Parse fails
func TestSynthesizeDocParseError(t *testing.T) {
	// ir.Parse fails when the JSON is valid but validation fails.
	// Create minimal IR that fails validation - e.g., a resource with an invalid name.
	data, err := ir.Parse([]byte(`{"project":"p","plugins":[],"resources":{"":{"type":"container","plugin":"docker","properties":{},"depends_on":[]}}}`))
	if err == nil {
		t.Fatal("Expected ir.Parse error for empty resource name, got nil")
	}
	if data != nil {
		t.Fatal("ir.Parse returned non-nil document on error")
	}
	// Also test: invalid plugin name (spaces not allowed by PluginNamePattern)
	_, err2 := ir.Parse([]byte(`{"project":"p","plugins":[],"resources":{"a":{"type":"container","plugin":"bad plugin","properties":{},"depends_on":[]}}}`))
	if err2 == nil {
		t.Fatal("Expected ir.Parse error for invalid plugin name")
	}
}

// TestInstallSpecAssetMissingFromChecksum: asset matches but checksums.txt doesn't have it
func TestInstallSpecAssetMissingFromChecksum(t *testing.T) {
	dir := t.TempDir()
	handler := func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/repos/") || strings.Contains(path, "latest") || strings.Contains(path, "tags/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			// Include checksums.txt in release but make it not include one of the assets
			base := requestBase(r)
			body := fmt.Sprintf(`{"tag_name":"v1","assets":[{"name":"checksums.txt","browser_download_url":"%s/assets/checksums.txt","size":1},{"name":"dex-adapter-docker_%s_%s","browser_download_url":"%s/assets/dex-adapter-docker_%s_%s","size":1},{"name":"dex-adapter-proxmox_%s_%s","browser_download_url":"%s/assets/dex-adapter-proxmox_%s_%s","size":1}]}`, base, assetOS(), assetArch(), base, assetOS(), assetArch(), assetOS(), assetArch(), base, assetOS(), assetArch())
			io.WriteString(w, body)
			return
		}
		if strings.Contains(path, "/assets/checksums.txt") {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			// Only include docker, NOT proxmox
			h := sha256.Sum256([]byte("dex-adapter-docker_" + assetOS() + "_" + assetArch() + "dex-adapter-docker_" + assetOS() + "_" + assetArch() + "dex-adapter-docker_" + assetOS() + "_" + assetArch()))
			var buf []byte
			for i := range h {
				buf = append(buf, h[i])
			}
			io.WriteString(w, hex.EncodeToString(buf)+"  dex-adapter-docker_"+assetOS()+"_"+assetArch()+"\n")
			return
		}
		if strings.Contains(path, "/assets/") {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(200)
			namePart := path[len("/assets/"):]
			io.WriteString(w, namePart+namePart+namePart)
			return
		}
		w.WriteHeader(404)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	var out bytes.Buffer
	err := installSpec(dir, "github.com/owner/repo", srv.URL, &out)
	if err == nil {
		t.Fatalf("Expected error (proxmox missing from checksums), got nil, out=%q", out.String())
	}
	if !strings.Contains(out.String(), "installed dex-adapter-docker") {
		t.Fatalf("docker should have been installed, out=%q", out.String())
	}
	if !strings.Contains(err.Error(), "not found in checksums.txt") {
		t.Fatalf("err = %v, want 'not found in checksums.txt'", err)
	}
}
