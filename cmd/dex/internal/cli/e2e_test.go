//go:build docker

package cli

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// End-to-end through the real CLI: esbuild + goja + the embedded SDK, file
// state, and the Docker adapter against the daemon named by DOCKER_HOST.
//
//	go test -tags docker ./cmd/dex/...

const e2eImage = "traefik/whoami:latest"

type e2eProject struct {
	t    *testing.T
	dir  string
	path string // saved PATH to restore on cleanup
}

func newE2EProject(t *testing.T) *e2eProject {
	t.Helper()
	p := &e2eProject{t: t, dir: t.TempDir()}
	// Add bin/ to PATH so dex apply can find dex-adapter-docker.
	oldPath := os.Getenv("PATH")
	p.path = oldPath
	binDir, _ := filepath.Abs("bin")
	os.Setenv("PATH", binDir+":"+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	if code, out := p.dex("init"); code != 0 {
		t.Fatalf("dex init = %d: %s", code, out)
	}
	return p
}

// config rewrites dex.config.ts and writes one dex/services/<name>.ts file
// per service; each service maps a name to its property object literal
// (TypeScript source).
func (p *e2eProject) config(services map[string]string) {
	p.t.Helper()
	config := "import { defineDexConfig } from \"@dex/iac\";\n\ndefineDexConfig({ project: \"e2e\" });\n"
	if err := os.WriteFile(filepath.Join(p.dir, "dex.config.ts"), []byte(config), 0o644); err != nil {
		p.t.Fatal(err)
	}
	servicesDir := filepath.Join(p.dir, "dex", "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		p.t.Fatal(err)
	}
	for name, props := range services {
		body := fmt.Sprintf("import { service } from \"@dex/iac\";\n\nservice(%q, { plugin: \"docker\", properties: %s });\n", name, props)
		if err := os.WriteFile(filepath.Join(servicesDir, name+".ts"), []byte(body), 0o644); err != nil {
			p.t.Fatal(err)
		}
	}
}

func (p *e2eProject) dex(args ...string) (int, string) {
	var out, errOut bytes.Buffer
	code := Main(append(args, "--dir", p.dir), strings.NewReader(""), &out, &errOut)
	return code, out.String() + errOut.String()
}

func (p *e2eProject) apply() string {
	p.t.Helper()
	code, out := p.dex("apply", "--yes")
	if code != 0 {
		p.t.Fatalf("dex apply = %d: %s", code, out)
	}
	return out
}

func dockerCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func containerField(t *testing.T, name, format string) string {
	t.Helper()
	return dockerCLI(t, "inspect", name, "--format", format)
}

func containerExists(name string) bool {
	return exec.Command("docker", "inspect", name).Run() == nil
}

func e2eFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func answers(port int) bool {
	c := http.Client{Timeout: 2 * time.Second}
	for range 10 {
		if resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d", port)); err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func whoamiProps(port int, restart, command string) string {
	props := fmt.Sprintf("{ image: %q, ports: [\"%d:80\"], restart: %q", e2eImage, port, restart)
	if command != "" {
		props += ", command: " + command
	}
	return props + " }"
}

func TestE2ELifecycle(t *testing.T) {
	name := fmt.Sprintf("dex-e2e-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	p := newE2EProject(t)
	port := e2eFreePort(t)
	set := func(restart, command string) { p.config(map[string]string{name: whoamiProps(port, restart, command)}) }

	set("no", "")
	if out := p.apply(); !strings.Contains(out, "1 added") || !answers(port) {
		t.Fatalf("create: %s; port %d answering = %v", out, port, answers(port))
	}
	if got := containerField(t, name, `{{index .Config.Labels "dev.dex.resource"}}`); got != name {
		t.Fatalf("label = %q", got)
	}

	id := containerField(t, name, "{{.Id}}")
	set("always", "")
	if out := p.apply(); !strings.Contains(out, "~ "+name+" (restart)") || containerField(t, name, "{{.Id}}") != id {
		t.Fatalf("restart-only update replaced the container: %s", out)
	}

	dockerCLI(t, "rm", "-f", name)
	set("no", "")
	p.apply()
	if containerField(t, name, "{{.State.Running}}") != "true" {
		t.Fatal("restart-only update did not recreate a container removed by hand")
	}

	set("no", `["--no-such-flag"]`)
	if code, out := p.dex("apply", "--yes"); code != 1 || !strings.Contains(out, "exit code 2") {
		t.Fatalf("crashing command: code %d, %s", code, out)
	}
	if containerField(t, name, "{{.State.Running}}") != "false" {
		t.Fatal("crashed container should be left in place, stopped")
	}

	// Reverting to the last good config must replace the tainted resource,
	// not plan "No changes" while nothing runs.
	set("no", "")
	if out := p.apply(); !strings.Contains(out, "(tainted: last apply failed)") || !answers(port) {
		t.Fatalf("revert after crash: out=%q answers=%v", out, answers(port))
	}

	p.config(nil)
	if out := p.apply(); !strings.Contains(out, "1 destroyed") || containerExists(name) {
		t.Fatalf("delete: %s; container still exists = %v", out, containerExists(name))
	}
}

func TestE2EForeignContainerIsNeverTouched(t *testing.T) {
	name := fmt.Sprintf("dex-e2e-foreign-%d", time.Now().UnixNano())
	dockerCLI(t, "run", "-d", "--name", name, e2eImage)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	p := newE2EProject(t)

	p.config(map[string]string{name: fmt.Sprintf("{ image: %q }", e2eImage)})
	if code, out := p.dex("apply", "--yes"); code != 1 || !strings.Contains(out, "not managed by dex") {
		t.Fatalf("create over foreign container: code %d, %s", code, out)
	}

	// The failed create left the resource tainted, so re-applying the same
	// config plans a replacement. Its delete must leave the foreign container
	// running; its create fails again.
	if code, out := p.dex("apply", "--yes"); code != 1 || !strings.Contains(out, "(tainted: last apply failed)") {
		t.Fatalf("re-apply over foreign container: code %d, %q", code, out)
	}
	if containerField(t, name, "{{.State.Running}}") != "true" || containerField(t, name, `{{index .Config.Labels "dev.dex.resource"}}`) != "" {
		t.Fatal("dex removed or replaced a container it did not create")
	}

	// Once the user clears the blocker, the same config must create the
	// container rather than plan "No changes" for the tainted resource.
	dockerCLI(t, "rm", "-f", name)
	if out := p.apply(); !strings.Contains(out, "1 added") || containerField(t, name, "{{.State.Running}}") != "true" {
		t.Fatalf("apply after removing the blocker: %q", out)
	}

	p.config(nil)
	if out := p.apply(); !strings.Contains(out, "1 destroyed") || containerExists(name) {
		t.Fatalf("delete: %q", out)
	}
}
