// Package docker is the in-process Docker adapter: it materializes
// `container` resources on the Docker daemon named by DOCKER_HOST.
package docker

import (
	"context"
	"fmt"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/freality/dexter/pkg/plugin"
	"github.com/moby/moby/client"
)

// API is the subset of the Docker client the adapter uses. *client.Client
// satisfies it; Fake implements it for tests.
type API interface {
	ImageInspect(ctx context.Context, ref string, opts ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ImagePull(ctx context.Context, ref string, opts client.ImagePullOptions) (client.ImagePullResponse, error)
	ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, name string, opts client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(ctx context.Context, name string, opts client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRemove(ctx context.Context, name string, opts client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerInspect(ctx context.Context, name string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerUpdate(ctx context.Context, name string, opts client.ContainerUpdateOptions) (client.ContainerUpdateResult, error)
}

// Adapter creates, updates, and deletes containers through an API.
// Containers are addressed by resource name, never by stored id.
type Adapter struct {
	api API
	// ponytail: fixed delay before one crash check catches containers that
	// die at once, not ones that crash after a slower init. Upgrade to a
	// short poll or a real health check if that proves too short.
	settle time.Duration
}

// NewWithAPI returns an Adapter backed by api.
func NewWithAPI(api API) *Adapter {
	return &Adapter{api: api, settle: 500 * time.Millisecond}
}

// New returns an Adapter for the daemon named by DOCKER_HOST (default: the
// local socket). No connection is made until the first call.
func New() (*Adapter, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return NewWithAPI(c), nil
}

// Create materializes resource name and returns its outputs ({"id": ...}).
func (a *Adapter) Create(ctx context.Context, name, typ string, props map[string]any, report plugin.Reporter) (map[string]any, error) {
	if typ != "container" {
		return nil, fmt.Errorf("docker: unsupported type %q", typ)
	}
	s, err := parseSpec(props)
	if err != nil {
		return nil, err
	}
	return a.launch(ctx, name, s, report)
}

// Update moves container name from properties cur to desired. A change to
// restart alone is applied in place; anything else replaces the container,
// pulling the new image first so a failed pull leaves the old one running.
func (a *Adapter) Update(ctx context.Context, name string, desired, cur map[string]any, report plugin.Reporter) (map[string]any, error) {
	s, err := parseSpec(desired)
	if err != nil {
		return nil, err
	}
	if old, err := parseSpec(cur); err == nil && old.sameExceptRestart(s) {
		return a.updateRestart(ctx, name, s, report)
	}
	if err := a.ensureImage(ctx, s.Image, report); err != nil {
		return nil, err
	}
	if _, err := a.removeOwned(ctx, name); err != nil {
		return nil, err
	}
	return a.launch(ctx, name, s, report)
}

// updateRestart changes only the restart policy, keeping the container. If
// the container was removed by hand it is recreated, so the change cannot
// fail the same way on every later apply.
func (a *Adapter) updateRestart(ctx context.Context, name string, s spec, report plugin.Reporter) (map[string]any, error) {
	policy := s.createOptions(name).HostConfig.RestartPolicy
	_, err := a.api.ContainerUpdate(ctx, name, client.ContainerUpdateOptions{RestartPolicy: &policy})
	if cerrdefs.IsNotFound(err) {
		return a.launch(ctx, name, s, report)
	}
	if err != nil {
		return nil, fmt.Errorf("update container %q: %w", name, err)
	}
	got, err := a.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", name, err)
	}
	return map[string]any{"id": got.Container.ID}, nil
}

// Delete removes container name. A free name, or one held by a container dex
// did not create, counts as deleted and is left alone.
func (a *Adapter) Delete(ctx context.Context, name string) error {
	_, err := a.removeOwned(ctx, name)
	return err
}

// ensureImage pulls ref only when the daemon does not already have it.
func (a *Adapter) ensureImage(ctx context.Context, ref string, report plugin.Reporter) error {
	_, err := a.api.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}
	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %q: %w", ref, err)
	}
	resp, err := a.api.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer resp.Close()

	type layerProgress struct{ current, total int64 }
	layers := map[string]layerProgress{}
	for msg, err := range resp.JSONMessages(ctx) {
		if err != nil {
			return fmt.Errorf("pull image %q: %w", ref, err)
		}
		if msg.ID != "" {
			lp := layers[msg.ID]
			if msg.Progress != nil {
				lp.current, lp.total = msg.Progress.Current, msg.Progress.Total
			}
			if msg.Status == "Pull complete" || msg.Status == "Already exists" {
				lp.current = lp.total
			}
			layers[msg.ID] = lp
		}
		var curSum, totalSum int64
		for _, lp := range layers {
			curSum += lp.current
			totalSum += lp.total
		}
		if report != nil {
			if msg.Status != "" {
				report.Log(msg.Status)
			}
			if totalSum > 0 {
				report.Progress(int(curSum * 100 / totalSum))
			}
		}
	}
	return nil
}

// launch pulls s.Image if missing, creates and starts container name from s,
// then checks it is still
// running after the settle delay. A crashed container is left in place for
// `docker logs`; the next Create claims it as an orphan.
func (a *Adapter) launch(ctx context.Context, name string, s spec, report plugin.Reporter) (map[string]any, error) {
	if err := a.ensureImage(ctx, s.Image, report); err != nil {
		return nil, err
	}
	res, err := a.api.ContainerCreate(ctx, s.createOptions(name))
	if cerrdefs.IsConflict(err) {
		// Name taken: claim it if it is an orphan from an apply that died
		// before saving state; refuse anything dex did not create.
		foreign, claimErr := a.removeOwned(ctx, name)
		if claimErr != nil {
			return nil, claimErr
		}
		if foreign {
			return nil, fmt.Errorf("container %q exists and is not managed by dex", name)
		}
		res, err = a.api.ContainerCreate(ctx, s.createOptions(name))
	}
	if err != nil {
		return nil, fmt.Errorf("create container %q: %w", name, err)
	}
	if _, err = a.api.ContainerStart(ctx, name, client.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("start container %q: %w", name, err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("start container %q: %w", name, ctx.Err())
	case <-time.After(a.settle):
	}
	got, err := a.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", name, err)
	}
	if st := got.Container.State; !st.Running || st.Restarting {
		return nil, fmt.Errorf("container %q is not running after start (exit code %d); see docker logs %s", name, st.ExitCode, name)
	}
	return map[string]any{"id": res.ID}, nil
}

// removeOwned stops (Docker's default timeout) and removes container name if
// dex created it. It reports foreign, touching nothing, when name is held by
// a container without dex's label; a free name is a no-op.
func (a *Adapter) removeOwned(ctx context.Context, name string) (foreign bool, err error) {
	got, err := a.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect container %q: %w", name, err)
	}
	if got.Container.Config.Labels[labelResource] != name {
		return true, nil
	}
	if _, err := a.api.ContainerStop(ctx, name, client.ContainerStopOptions{}); err != nil {
		return false, fmt.Errorf("stop container %q: %w", name, err)
	}
	if _, err := a.api.ContainerRemove(ctx, name, client.ContainerRemoveOptions{}); err != nil {
		return false, fmt.Errorf("remove container %q: %w", name, err)
	}
	return false, nil
}
