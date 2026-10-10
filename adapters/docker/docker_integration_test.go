//go:build docker

package docker

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Runs against the daemon named by DOCKER_HOST (default: the local socket):
//
//	go test -tags docker ./adapters/docker/...
//
// It checks what Fake can only imitate: the real daemon's not-found and
// name-conflict errors, image pulls, crash detection, and container ids.

const itImage = "traefik/whoami:latest"

func TestIntegrationLifecycle(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := fmt.Sprintf("dex-it-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = a.Delete(context.Background(), name) })

	props := func(restart string, port int, cmd ...string) map[string]any {
		p := map[string]any{"image": itImage, "ports": []any{fmt.Sprintf("%d:80", port)}, "restart": restart}
		if len(cmd) > 0 {
			c := make([]any, len(cmd))
			for i, s := range cmd {
				c[i] = s
			}
			p["command"] = c
		}
		return p
	}
	port1, port2 := freePort(t), freePort(t)

	out, err := a.Create(ctx, name, "container", props("no", port1))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := out["id"].(string)
	got := inspect(t, a, name)
	if got.ID != id || !got.State.Running || got.Config.Labels[labelResource] != name {
		t.Fatalf("created container = id %s running %v labels %v", got.ID, got.State.Running, got.Config.Labels)
	}

	// Restart-only change keeps the container.
	out, err = a.Update(ctx, name, props("always", port1), props("no", port1))
	if err != nil {
		t.Fatalf("Update(restart) error = %v", err)
	}
	if got := inspect(t, a, name); got.ID != id || out["id"] != id || got.HostConfig.RestartPolicy.Name != "always" {
		t.Fatalf("after restart-only update: id %s (want %s), outputs %v, restart %q", got.ID, id, out, got.HostConfig.RestartPolicy.Name)
	}

	// Any other change replaces it.
	out, err = a.Update(ctx, name, props("always", port2), props("always", port1))
	if err != nil {
		t.Fatalf("Update(ports) error = %v", err)
	}
	if got := inspect(t, a, name); got.ID == id || out["id"] != got.ID || !got.State.Running {
		t.Fatalf("after replace: id %s (old %s), outputs %v, running %v", got.ID, id, out, got.State.Running)
	}

	// Create over our own container claims it (real name-conflict error).
	id = out["id"].(string)
	out, err = a.Create(ctx, name, "container", props("always", port2))
	if err != nil || out["id"] == id {
		t.Fatalf("Create() over orphan = %v, %v; want a new container", out, err)
	}

	// Restart-only change after a manual `docker rm` recreates it (real
	// not-found from ContainerUpdate).
	if _, err := a.api.ContainerRemove(ctx, name, client.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Update(ctx, name, props("no", port2), props("always", port2)); err != nil {
		t.Fatalf("Update() of removed container error = %v", err)
	}
	if got := inspect(t, a, name); !got.State.Running {
		t.Fatal("container not recreated")
	}

	// A container that dies at once fails Create and is left for docker logs.
	// command is Docker's Cmd (the entrypoint's arguments, as in compose), so
	// this runs `/whoami --no-such-flag`, which exits 2.
	if err := a.Delete(ctx, name); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, err = a.Create(ctx, name, "container", props("no", port2, "--no-such-flag"))
	if err == nil || !strings.Contains(err.Error(), "is not running after start (exit code 2)") {
		t.Fatalf("Create() of crashing container error = %v", err)
	}
	if got := inspect(t, a, name); got.State.Running {
		t.Fatal("crashed container reported running")
	}

	// Delete removes it, and deleting again is a no-op (real not-found).
	if err := a.Delete(ctx, name); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := a.Delete(ctx, name); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
}

func TestIntegrationRefusesForeignContainer(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := fmt.Sprintf("dex-it-foreign-%d", time.Now().UnixNano())
	if err := a.ensureImage(ctx, itImage); err != nil {
		t.Fatal(err)
	}
	if _, err := a.api.ContainerCreate(ctx, client.ContainerCreateOptions{Name: name, Config: &container.Config{Image: itImage}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = a.api.ContainerRemove(context.Background(), name, client.ContainerRemoveOptions{Force: true})
	})

	_, err = a.Create(ctx, name, "container", map[string]any{"image": itImage})
	if err == nil || err.Error() != fmt.Sprintf("container %q exists and is not managed by dex", name) {
		t.Fatalf("Create() error = %v", err)
	}

	// Delete (e.g. replacing a resource tainted by that failed Create) must
	// leave the foreign container alone.
	if err := a.Delete(ctx, name); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	inspect(t, a, name)
}

func inspect(t *testing.T, a *Adapter, name string) container.InspectResponse {
	t.Helper()
	res, err := a.api.ContainerInspect(context.Background(), name, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect %s: %v", name, err)
	}
	return res.Container
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
