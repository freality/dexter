package docker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func newTestAdapter(f *Fake) *Adapter {
	a := NewWithAPI(f)
	a.settle = 0
	return a
}

func whoami() map[string]any {
	return map[string]any{
		"image": "traefik/whoami:latest",
		"ports": []any{"8080:80"},
		"env":   map[string]any{"TZ": "UTC"},
	}
}

func TestCreatePullsMissingImageAndStartsLabelledContainer(t *testing.T) {
	f := NewFake()
	a := newTestAdapter(f)

	out, err := a.Create(context.Background(), "whoami", "container", whoami())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	c := f.Containers["whoami"]
	if c == nil || !c.Running {
		t.Fatalf("container whoami = %+v, want running", c)
	}
	if out["id"] != c.ID {
		t.Fatalf("outputs = %v, want id %q", out, c.ID)
	}
	if !f.Images["traefik/whoami:latest"] {
		t.Fatal("image was not pulled")
	}
	if got := c.Config.Labels["dev.dex.resource"]; got != "whoami" {
		t.Fatalf("label dev.dex.resource = %q, want whoami", got)
	}
	if c.Config.Image != "traefik/whoami:latest" || !slices.Equal(c.Config.Env, []string{"TZ=UTC"}) {
		t.Fatalf("config = %+v", c.Config)
	}
	if b := c.HostConfig.PortBindings; len(b) != 1 || b[mustPort(t, "80/tcp")][0].HostPort != "8080" {
		t.Fatalf("port bindings = %v, want 8080->80/tcp", b)
	}
	if c.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Fatalf("restart = %q, want unless-stopped default", c.HostConfig.RestartPolicy.Name)
	}
}

func mustPort(t *testing.T, s string) network.Port {
	t.Helper()
	p, err := network.ParsePort(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateSkipsPullWhenImagePresent(t *testing.T) {
	f := NewFake()
	f.Images["traefik/whoami:latest"] = true

	if _, err := newTestAdapter(f).Create(context.Background(), "whoami", "container", whoami()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if slices.Contains(f.Calls, "ImagePull") {
		t.Fatalf("calls = %v, want no ImagePull", f.Calls)
	}
}

func TestCreateRejectsUnsupportedType(t *testing.T) {
	f := NewFake()
	_, err := newTestAdapter(f).Create(context.Background(), "data", "volume", map[string]any{})
	if err == nil || err.Error() != `docker: unsupported type "volume"` {
		t.Fatalf("Create() error = %v", err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("calls = %v, want none", f.Calls)
	}
}

func TestCreateAcceptsOptionalProperties(t *testing.T) {
	f := NewFake()
	props := map[string]any{
		"image":   "postgres:16",
		"command": []any{"postgres", "-c", "fsync=off"},
		"restart": "always",
	}
	if _, err := newTestAdapter(f).Create(context.Background(), "db", "container", props); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	c := f.Containers["db"]
	if len(c.HostConfig.PortBindings) != 0 || len(c.Config.ExposedPorts) != 0 {
		t.Fatalf("ports = %v, want none published", c.HostConfig.PortBindings)
	}
	if !slices.Equal(c.Config.Cmd, []string{"postgres", "-c", "fsync=off"}) {
		t.Fatalf("cmd = %v", c.Config.Cmd)
	}
	if c.HostConfig.RestartPolicy.Name != "always" {
		t.Fatalf("restart = %q, want always", c.HostConfig.RestartPolicy.Name)
	}
}

func TestCreateRejectsInvalidProperties(t *testing.T) {
	with := func(k string, v any) map[string]any {
		p := whoami()
		if v == nil {
			delete(p, k)
		} else {
			p[k] = v
		}
		return p
	}
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"missing image", with("image", nil), `property "image": required string`},
		{"empty image", with("image", ""), `property "image": required string`},
		{"image not string", with("image", 1.0), `property "image": required string`},
		{"unknown key", with("volumes", []any{}), `property "volumes": unknown (supported: command, env, image, ports, restart)`},
		{"ports not list", with("ports", "8080:80"), `property "ports": must be a list of strings`},
		{"port not string", with("ports", []any{8080.0}), `property "ports": must be a list of strings`},
		{"port without colon", with("ports", []any{"8080"}), `property "ports": "8080" must be "host:container[/proto]"`},
		{"host port not numeric", with("ports", []any{"x:80"}), `property "ports": "x:80" has invalid host port "x"`},
		{"host port zero", with("ports", []any{"0:80"}), `property "ports": "0:80" has invalid host port "0"`},
		{"host port too big", with("ports", []any{"70000:80"}), `property "ports": "70000:80" has invalid host port "70000"`},
		{"container port invalid", with("ports", []any{"8080:http"}), `property "ports": "8080:http" has invalid container port "http"`},
		{"env not object", with("env", []any{"TZ=UTC"}), `property "env": must be an object of strings`},
		{"env value not string", with("env", map[string]any{"N": 1.0}), `property "env": value of "N" must be a string`},
		{"command not list", with("command", "sh -c true"), `property "command": must be a list of strings`},
		{"command entry not string", with("command", []any{"run", 1.0}), `property "command": must be a list of strings`},
		{"restart not string", with("restart", true), `property "restart": must be one of no, always, on-failure, unless-stopped`},
		{"restart unknown", with("restart", "sometimes"), `property "restart": must be one of no, always, on-failure, unless-stopped`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFake()
			_, err := newTestAdapter(f).Create(context.Background(), "whoami", "container", tc.props)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Create() error = %v, want %q", err, tc.want)
			}
			if len(f.Calls) != 0 {
				t.Fatalf("calls = %v, want none before validation passes", f.Calls)
			}
		})
	}
}

func TestCreateReplacesOrphanedDexContainer(t *testing.T) {
	f := NewFake()
	a := newTestAdapter(f)
	if _, err := a.Create(context.Background(), "whoami", "container", whoami()); err != nil {
		t.Fatal(err)
	}
	orphan := f.Containers["whoami"].ID

	out, err := a.Create(context.Background(), "whoami", "container", whoami())
	if err != nil {
		t.Fatalf("Create() over orphan error = %v", err)
	}
	if c := f.Containers["whoami"]; c.ID == orphan || out["id"] != c.ID || !c.Running {
		t.Fatalf("container = %+v, outputs = %v; want a new running container replacing %s", c, out, orphan)
	}
}

func TestCreateRefusesForeignContainer(t *testing.T) {
	f := NewFake()
	f.Containers["whoami"] = &FakeContainer{ID: "theirs", Config: &container.Config{}, Running: true}

	_, err := newTestAdapter(f).Create(context.Background(), "whoami", "container", whoami())
	if err == nil || err.Error() != `container "whoami" exists and is not managed by dex` {
		t.Fatalf("Create() error = %v", err)
	}
	if c := f.Containers["whoami"]; c.ID != "theirs" || !c.Running {
		t.Fatalf("foreign container touched: %+v", c)
	}
}

func TestCreateFailsWhenContainerExitsRightAfterStart(t *testing.T) {
	f := NewFake()
	f.CrashOnStart = true

	_, err := newTestAdapter(f).Create(context.Background(), "whoami", "container", whoami())
	want := `container "whoami" is not running after start (exit code 1); see docker logs whoami`
	if err == nil || err.Error() != want {
		t.Fatalf("Create() error = %v, want %q", err, want)
	}
	if _, ok := f.Containers["whoami"]; !ok {
		t.Fatal("crashed container was removed; want it left for docker logs")
	}
}

func TestCreateStopsWaitingWhenContextIsCancelled(t *testing.T) {
	f := NewFake()
	a := NewWithAPI(f)
	a.settle = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Create(ctx, "whoami", "container", whoami())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}
}

func TestCreateWrapsDockerErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		method   string
		existing bool // a dex container already holds the name
		want     string
	}{
		{"ImageInspect", false, `inspect image "traefik/whoami:latest": boom`},
		{"ImagePull", false, `pull image "traefik/whoami:latest": boom`},
		{"ImagePull.Wait", false, `pull image "traefik/whoami:latest": boom`},
		{"ContainerCreate", false, `create container "whoami": boom`},
		{"ContainerStart", false, `start container "whoami": boom`},
		{"ContainerInspect", false, `inspect container "whoami": boom`},
		{"ContainerInspect", true, `inspect container "whoami": boom`},
		{"ContainerStop", true, `stop container "whoami": boom`},
		{"ContainerRemove", true, `remove container "whoami": boom`},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			f := NewFake()
			if tc.existing {
				f.Images["traefik/whoami:latest"] = true
				f.Containers["whoami"] = &FakeContainer{ID: "old", Config: &container.Config{Labels: map[string]string{"dev.dex.resource": "whoami"}}}
			}
			f.Err[tc.method] = boom
			_, err := newTestAdapter(f).Create(context.Background(), "whoami", "container", whoami())
			if !errors.Is(err, boom) || err.Error() != tc.want {
				t.Fatalf("Create() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// created returns an adapter whose fake already runs whoami() as "whoami".
func created(t *testing.T) (*Adapter, *Fake, string) {
	t.Helper()
	f := NewFake()
	a := newTestAdapter(f)
	out, err := a.Create(context.Background(), "whoami", "container", whoami())
	if err != nil {
		t.Fatal(err)
	}
	f.Calls = nil
	return a, f, out["id"].(string)
}

func TestUpdateRestartOnlyChangesPolicyInPlace(t *testing.T) {
	a, f, id := created(t)
	desired := whoami()
	desired["restart"] = "always"

	out, err := a.Update(context.Background(), "whoami", desired, whoami())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	c := f.Containers["whoami"]
	if c.ID != id || out["id"] != id {
		t.Fatalf("id = %s, outputs = %v; want container %s kept", c.ID, out, id)
	}
	if c.HostConfig.RestartPolicy.Name != "always" {
		t.Fatalf("restart = %q, want always", c.HostConfig.RestartPolicy.Name)
	}
	if slices.Contains(f.Calls, "ContainerCreate") {
		t.Fatalf("calls = %v, want no recreate", f.Calls)
	}
}

func TestUpdateRestartOnlyRecreatesMissingContainer(t *testing.T) {
	a, f, id := created(t)
	delete(f.Containers, "whoami") // removed by hand
	desired := whoami()
	desired["restart"] = "always"

	out, err := a.Update(context.Background(), "whoami", desired, whoami())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	c := f.Containers["whoami"]
	if c == nil || !c.Running || c.ID == id || out["id"] != c.ID {
		t.Fatalf("container = %+v, outputs = %v; want a new running container", c, out)
	}
	if c.HostConfig.RestartPolicy.Name != "always" {
		t.Fatalf("restart = %q, want always", c.HostConfig.RestartPolicy.Name)
	}
}

func TestUpdateOtherChangeReplacesContainer(t *testing.T) {
	a, f, id := created(t)
	desired := whoami()
	desired["ports"] = []any{"8081:80"}

	out, err := a.Update(context.Background(), "whoami", desired, whoami())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	c := f.Containers["whoami"]
	if c.ID == id || out["id"] != c.ID || !c.Running {
		t.Fatalf("container = %+v, outputs = %v; want replacement of %s", c, out, id)
	}
	if got := c.HostConfig.PortBindings[mustPort(t, "80/tcp")][0].HostPort; got != "8081" {
		t.Fatalf("host port = %s, want 8081", got)
	}
}

func TestUpdateReplacesWhenSavedPropertiesNoLongerParse(t *testing.T) {
	a, f, id := created(t)
	cur := whoami()
	cur["legacy"] = true

	if _, err := a.Update(context.Background(), "whoami", whoami(), cur); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if f.Containers["whoami"].ID == id {
		t.Fatal("container kept; want replacement when current properties are unreadable")
	}
}

func TestUpdatePullsNewImageBeforeRemovingOldContainer(t *testing.T) {
	a, f, id := created(t)
	f.Err["ImagePull"] = errors.New("registry down")
	desired := whoami()
	desired["image"] = "traefik/whoami:v1.10"

	if _, err := a.Update(context.Background(), "whoami", desired, whoami()); err == nil {
		t.Fatal("Update() error = nil, want pull failure")
	}
	if c := f.Containers["whoami"]; c == nil || c.ID != id || !c.Running {
		t.Fatalf("container = %+v; want old container %s still running", c, id)
	}
}

func TestUpdateRejectsInvalidDesiredProperties(t *testing.T) {
	a, f, _ := created(t)
	_, err := a.Update(context.Background(), "whoami", map[string]any{}, whoami())
	if err == nil || err.Error() != `property "image": required string` {
		t.Fatalf("Update() error = %v", err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("calls = %v, want none", f.Calls)
	}
}

func TestUpdateWrapsDockerErrors(t *testing.T) {
	boom := errors.New("boom")
	restartOnly := whoami()
	restartOnly["restart"] = "no"
	replace := whoami()
	replace["ports"] = []any{"9090:80"}
	cases := []struct {
		name, method string
		desired      map[string]any
		want         string
	}{
		{"update", "ContainerUpdate", restartOnly, `update container "whoami": boom`},
		{"inspect after update", "ContainerInspect", restartOnly, `inspect container "whoami": boom`},
		{"stop before replace", "ContainerStop", replace, `stop container "whoami": boom`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, f, _ := created(t)
			f.Err[tc.method] = boom
			_, err := a.Update(context.Background(), "whoami", tc.desired, whoami())
			if !errors.Is(err, boom) || err.Error() != tc.want {
				t.Fatalf("Update() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDeleteRemovesContainer(t *testing.T) {
	a, f, _ := created(t)
	if err := a.Delete(context.Background(), "whoami"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := f.Containers["whoami"]; ok {
		t.Fatal("container still present")
	}
}

func TestDeleteToleratesMissingContainer(t *testing.T) {
	if err := newTestAdapter(NewFake()).Delete(context.Background(), "whoami"); err != nil {
		t.Fatalf("Delete() error = %v, want nil for a container removed by hand", err)
	}
}

func TestDeleteWrapsRemoveError(t *testing.T) {
	a, f, _ := created(t)
	boom := errors.New("boom")
	f.Err["ContainerRemove"] = boom
	if err := a.Delete(context.Background(), "whoami"); !errors.Is(err, boom) || err.Error() != `remove container "whoami": boom` {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestNewReadsDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")
	if a, err := New(); err != nil || a == nil {
		t.Fatalf("New() = %v, %v", a, err)
	}

	t.Setenv("DOCKER_HOST", "::not-a-host")
	if _, err := New(); err == nil || !strings.HasPrefix(err.Error(), "docker client: ") {
		t.Fatalf("New() error = %v, want docker client error", err)
	}
}

func TestUpdateRestartOnlyReportsPullFailureForMissingContainer(t *testing.T) {
	a, f, _ := created(t)
	delete(f.Containers, "whoami")
	delete(f.Images, "traefik/whoami:latest")
	f.Err["ImagePull"] = errors.New("registry down")
	desired := whoami()
	desired["restart"] = "always"

	_, err := a.Update(context.Background(), "whoami", desired, whoami())
	if err == nil || err.Error() != `pull image "traefik/whoami:latest": registry down` {
		t.Fatalf("Update() error = %v", err)
	}
}

// foreign returns a fake holding a running container named whoami that dex
// did not create.
func foreign() *Fake {
	f := NewFake()
	f.Containers["whoami"] = &FakeContainer{ID: "theirs", Config: &container.Config{}, Running: true}
	return f
}

func TestDeleteLeavesForeignContainer(t *testing.T) {
	f := foreign()
	if err := newTestAdapter(f).Delete(context.Background(), "whoami"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if c := f.Containers["whoami"]; c == nil || !c.Running {
		t.Fatal("Delete() removed a container dex did not create")
	}
}

func TestUpdateReplaceRefusesForeignContainer(t *testing.T) {
	f := foreign()
	desired := whoami()
	desired["ports"] = []any{"8081:80"}

	_, err := newTestAdapter(f).Update(context.Background(), "whoami", desired, whoami())
	if err == nil || err.Error() != `container "whoami" exists and is not managed by dex` {
		t.Fatalf("Update() error = %v", err)
	}
	if c := f.Containers["whoami"]; c == nil || c.ID != "theirs" || !c.Running {
		t.Fatal("Update() replaced a container dex did not create")
	}
}
