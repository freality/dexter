// Package cli implements the dex Cobra commands. plugin.go provides the
// "dex plugin add" and "dex plugin install" subcommands for managing
// adapter plugin binaries from GitHub releases.
package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/jsenv"
)

// --- public: newPluginCmd ------------------------------------------------

// newPluginCmd returns a "plugin" cobra sub-command with "add" and "install"
// sub-sub-commands. Callers register it via root.AddCommand.
func newPluginCmd(dir *string, d deps) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage plugin adapters",
	}

	addCmd := &cobra.Command{
		Use:   "add <spec>",
		Short: "Install a plugin adapter from a GitHub release",
		Long:  "Usage: dex plugin add github.com/<owner>/<repo>[@tag]\n\nInstalls adapter binaries from a GitHub release.\nThe tag defaults to 'latest' when omitted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: dex plugin add github.com/<owner>/<repo>[@tag]")
			}
			spec := args[0]
			if err := installSpec(*dir, spec, "https://api.github.com", cmd.OutOrStdout()); err != nil {
				return err
			}
			return nil
		},
	}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install all plugins declared in dex.config.ts",
		Long:  "Usage: dex plugin install\n\nReads the plugins declared in dex.config.ts via IR.Plugins\nand installs any not already satisfied.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginInstall(*dir, d, cmd.OutOrStdout())
		},
	}

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(installCmd)
	return rootCmd
}

// runPluginInstall synthesizes the project to read IR.Plugins and installs
// each declared spec.
func runPluginInstall(dir string, d deps, w io.Writer) error {
	e, err := d.newEngine(dir)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	desired, err := synthesizeDoc(e, dir)
	if err != nil {
		return err
	}
	if len(desired.Plugins) == 0 {
		fmt.Fprintln(w, "No plugins declared in dex.config.ts.")
		return nil
	}
	var errs []error
	for _, spec := range desired.Plugins {
		if installErr := installSpec(dir, spec, "https://api.github.com", w); installErr != nil {
			errs = append(errs, installErr)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// synthesizeDoc runs the engine's synthesizer and returns the parsed IR doc.
func synthesizeDoc(e *engine.Engine, dir string) (*ir.Document, error) {
	synth := jsenv.NewSynthesizer(jsenv.NewEsbuildBundler(), jsenv.NewGojaEvaluator())
	env := envMap()
	data, err := synth.Synth(context.Background(), dir, env)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}
	doc, err := ir.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse IR: %w", err)
	}
	return doc, nil
}

// --- spec parsing ---------------------------------------------------------

// PluginSpecPattern validates plugin spec strings (same pattern as SDK).
var PluginSpecPattern = regexp.MustCompile(`^github\.com/[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*(@[^@]+)?$`)

// specParts splits "github.com/<owner>/<repo>[@tag]" into (owner, repo, tag).
// tag is "" when no @tag is present; returns an error for malformed specs.
func specParts(spec string) (owner string, repo string, tag string, err error) {
	if !PluginSpecPattern.MatchString(spec) {
		return "", "", "", fmt.Errorf("invalid plugin spec %q: must match github.com/<owner>/<repo>[@tag]", spec)
	}
	rest := spec[10:] // after "github.com/"
	at := strings.Index(rest, "@")
	if at >= 0 {
		owner = rest[:at]
		tag = rest[at+1:]
	} else {
		owner = rest
		tag = ""
	}
	slash := strings.Index(owner, "/")
	repo = owner[slash+1:]
	owner = owner[:slash]
	return owner, repo, tag, nil
}

// --- installSpec ----------------------------------------------------------

// installSpec installs one plugin spec from a GitHub release. baseURL is the
// GitHub API base (injectable for tests; production is "https://api.github.com").
func installSpec(dir, spec, baseURL string, w io.Writer) error {
	owner, repo, tag, err := specParts(spec)
	if err != nil {
		return err
	}

	pluginsDir := filepath.Join(dir, ".dex", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", pluginsDir, err)
	}

	// Fetch release info.
	release, err := fetchRelease(baseURL, owner, repo, tag)
	if err != nil {
		return err
	}

	// Get checksums.
	checksumsURL := release.assetURL("checksums.txt")
	if checksumsURL == "" {
		return fmt.Errorf("release for %s/%s has no checksums.txt asset", owner, repo)
	}
	checksumsBody, err := downloadBytes(checksumsURL)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	checksums := parseChecksums(string(checksumsBody))

	// Determine which adapter assets to look for.
	goos := assetOS()
	goarch := assetArch()
	prefix := "dex-adapter-"
	suffix := fmt.Sprintf("_%s_%s", goos, goarch)

	// Find matching assets in the release.
	type assetInfo struct {
		name string
		url  string
	}
	var matching []assetInfo
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, suffix) && strings.HasPrefix(a.Name, prefix) {
			matching = append(matching, assetInfo{name: a.Name, url: a.URL})
		}
	}
	if len(matching) == 0 {
		return fmt.Errorf(
			"no adapter asset matching %q for %s/%s (looked for prefix %q suffix %q)",
			prefix+"*"+suffix, owner, repo, prefix, suffix,
		)
	}

	var installErrors []error
	for _, asset := range matching {
		expectedHash, ok := checksums[asset.name]
		if !ok {
			installErrors = append(installErrors, fmt.Errorf(
				"asset %q not found in checksums.txt", asset.name,
			))
			continue
		}

		dest := filepath.Join(pluginsDir, asset.name)
		if isAlreadyInstalled(dest, expectedHash) {
			fmt.Fprintf(w, "%s %s already installed\n", asset.name, release.TagName)
			continue
		}

		if err := downloadAndVerify(asset.url, dest, expectedHash); err != nil {
			installErrors = append(installErrors, err)
			continue
		}

		fmt.Fprintf(w, "installed %s %s\n", asset.name, release.TagName)
	}

	if len(installErrors) > 0 {
		return errors.Join(installErrors...)
	}
	return nil
}

// --- release info ---------------------------------------------------------

type releaseAsset struct {
	Name string
	URL  string
}

type releaseInfo struct {
	TagName string
	Assets  []releaseAsset
}

// assetURL finds the download URL for a named asset, or returns "".
func (r *releaseInfo) assetURL(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// fetchRelease gets release info from the GitHub API.
func fetchRelease(baseURL, owner, repo, tag string) (*releaseInfo, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/", baseURL, url.PathEscape(owner), url.PathEscape(repo))
	if tag == "" {
		apiURL += "latest"
	} else {
		apiURL += fmt.Sprintf("tags/%s", url.PathEscape(tag))
	}

	var body []byte
	for range 2 {
		var hdrs http.Header
		hdrs["Accept"] = []string{"application/vnd.github+json"}
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			hdrs["Authorization"] = []string{"Bearer " + token}
		}

		// Build request with headers and send with the default client.
		req, reqErr := http.NewRequest("GET", apiURL, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("create request: %w", reqErr)
		}
		for key, vals := range hdrs {
			for _, val := range vals {
				req.Header.Add(key, val)
			}
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch release %s: %w", apiURL, err)
		}
		defer resp.Body.Close()
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read release response: %w", err)
		}
		if resp.StatusCode == 200 {
			break
		}
		if resp.StatusCode == 404 {
			return nil, fmt.Errorf("release not found at %s", apiURL)
		}
		if resp.StatusCode == 403 {
			continue // retry once on rate limit
		}
		return nil, fmt.Errorf("fetch release %s: HTTP %d", apiURL, resp.StatusCode)
	}

	// Parse GitHub release JSON.
	type githubAsset struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}
	type githubRelease struct {
		TagName string        `json:"tag_name"`
		Assets  []githubAsset `json:"assets"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	var raw githubRelease
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse release JSON: %w", err)
	}

	result := &releaseInfo{TagName: raw.TagName}
	for _, a := range raw.Assets {
		result.Assets = append(result.Assets, releaseAsset{Name: a.Name, URL: a.URL})
	}
	return result, nil
}

// --- asset OS/arch helpers ------------------------------------------------

func assetOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

func assetArch() string {
	switch runtime.GOARCH {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

// --- checksums ------------------------------------------------------------

// isHexChar returns true when c is a valid hex digit.
func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isHexString returns true when every character in s is a hex digit.
func isHexString(s string) bool {
	for i := range s {
		if !isHexChar(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

// parseChecksums parses sha256sum output lines into a map of filename → hex hash.
func parseChecksums(text string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		if parts := strings.Fields(line); len(parts) >= 2 {
			hash := parts[0]
			name := parts[len(parts)-1]
			if len(hash) == 64 && isHexString(hash) {
				result[name] = hash
			}
		}
	}
	return result
}

// isAlreadyInstalled checks whether dest exists with the expected sha256 hash,
// returning true if it does. This makes re-running "dex plugin install" a
// no-op when checksums match.
func isAlreadyInstalled(dest, expectedHash string) bool {
	if _, err := os.Stat(dest); err != nil {
		return false
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return false
	}
	got := hex.EncodeToString(data)
	return got == expectedHash
}

// downloadAndVerify downloads a URL to a .tmp file, checks the sha256,
// then atomically renames to dest.
func downloadAndVerify(url, dest, expectedHash string) error {
	tmpPath := dest + ".tmp"
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	data, err := downloadBytes(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	hashBytes := sha256.Sum256(data)
	var hashSlice []byte
	for i := range hashBytes {
		hashSlice = append(hashSlice, hashBytes[i])
	}
	got := hex.EncodeToString(hashSlice)
	if got != expectedHash {
		return fmt.Errorf(
			"checksum mismatch for %s: expected %s, got %s",
			filepath.Base(dest), expectedHash[:12], got[:12],
		)
	}

	if err := os.WriteFile(tmpPath, data, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, dest, err)
	}
	return nil
}

// downloadBytes fetches a URL and returns its body as bytes.
func downloadBytes(urlStr string) ([]byte, error) {
	client := &http.Client{}
	req, reqErr := http.NewRequest("GET", urlStr, nil)
	if reqErr != nil {
		return nil, reqErr
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}