package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	s := fmt.Sprintf("%s://%s", r.URL.Scheme, r.URL.Host)
	if port := r.URL.Port(); port != "" && port != "0" {
		s += fmt.Sprintf(":%s", port)
	}
	return s
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
			io.WriteString(w, releaseBody(requestBase(r), tag, assets))
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
			io.WriteString(w, name + name + name)
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
			io.WriteString(w, releaseBody(requestBase(r), "v1.0.0", []string{"dex-adapter-docker_darwin_arm64"}))
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
	err := downloadAndVerify(srv.URL + "/data", dest, hash)
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
	err := downloadAndVerify(srv.URL + "/data", dest, "0000")
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
	if len(ri.Assets) != 1 {
		t.Fatalf("Assets = %v, want 1 asset", ri.Assets)
	}
	if ri.Assets[0].Name != "dex-adapter-foo_darwin_arm64" {
		t.Fatalf("asset name = %q", ri.Assets[0].Name)
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

// --- envMap is tested in root_test.go already --------------------------------