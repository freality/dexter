package docker

import (
	"context"
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Fake is an in-memory API: a minimal daemon holding images and named
// containers, for tests of this package and of its callers. Set Err[method]
// (e.g. Err["ContainerStart"]) to make that method fail.
type Fake struct {
	Images       map[string]bool
	Containers   map[string]*FakeContainer
	Err          map[string]error
	CrashOnStart bool     // started containers exit immediately with code 1
	Calls        []string // method names, in call order
	nextID       int
}

// FakeContainer is one container held by Fake, keyed by name.
type FakeContainer struct {
	ID         string
	Config     *container.Config
	HostConfig *container.HostConfig
	Running    bool
	ExitCode   int
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{Images: map[string]bool{}, Containers: map[string]*FakeContainer{}, Err: map[string]error{}}
}

func (f *Fake) call(method string) error {
	f.Calls = append(f.Calls, method)
	return f.Err[method]
}

func (f *Fake) find(name string) (*FakeContainer, error) {
	c, ok := f.Containers[name]
	if !ok {
		return nil, fmt.Errorf("no such container: %s: %w", name, cerrdefs.ErrNotFound)
	}
	return c, nil
}

func (f *Fake) ImageInspect(_ context.Context, ref string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	if err := f.call("ImageInspect"); err != nil {
		return client.ImageInspectResult{}, err
	}
	if !f.Images[ref] {
		return client.ImageInspectResult{}, fmt.Errorf("no such image: %s: %w", ref, cerrdefs.ErrNotFound)
	}
	return client.ImageInspectResult{}, nil
}

func (f *Fake) ImagePull(_ context.Context, ref string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
	if err := f.call("ImagePull"); err != nil {
		return nil, err
	}
	return &fakePull{f: f, ref: ref}, nil
}

// fakePull completes the pull on Wait. Embedding the interface satisfies its
// other methods, which the adapter never calls.
type fakePull struct {
	client.ImagePullResponse
	f   *Fake
	ref string
}

func (p *fakePull) Wait(context.Context) error {
	if err := p.f.call("ImagePull.Wait"); err != nil {
		return err
	}
	p.f.Images[p.ref] = true
	return nil
}

func (p *fakePull) Close() error { return nil }

func (f *Fake) ContainerCreate(_ context.Context, o client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	if err := f.call("ContainerCreate"); err != nil {
		return client.ContainerCreateResult{}, err
	}
	if _, ok := f.Containers[o.Name]; ok {
		return client.ContainerCreateResult{}, fmt.Errorf("name %q in use: %w", o.Name, cerrdefs.ErrConflict)
	}
	f.nextID++
	id := fmt.Sprintf("id-%d", f.nextID)
	f.Containers[o.Name] = &FakeContainer{ID: id, Config: o.Config, HostConfig: o.HostConfig}
	return client.ContainerCreateResult{ID: id}, nil
}

func (f *Fake) ContainerStart(_ context.Context, name string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	if err := f.call("ContainerStart"); err != nil {
		return client.ContainerStartResult{}, err
	}
	c := f.Containers[name] // the adapter only starts what it just created
	c.Running = !f.CrashOnStart
	if f.CrashOnStart {
		c.ExitCode = 1
	}
	return client.ContainerStartResult{}, nil
}

func (f *Fake) ContainerStop(_ context.Context, name string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	if err := f.call("ContainerStop"); err != nil {
		return client.ContainerStopResult{}, err
	}
	f.Containers[name].Running = false // stopped only after an inspect found it
	return client.ContainerStopResult{}, nil
}

func (f *Fake) ContainerRemove(_ context.Context, name string, _ client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	if err := f.call("ContainerRemove"); err != nil {
		return client.ContainerRemoveResult{}, err
	}
	// Removed only after an inspect found it and a stop succeeded.
	delete(f.Containers, name)
	return client.ContainerRemoveResult{}, nil
}

func (f *Fake) ContainerInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if err := f.call("ContainerInspect"); err != nil {
		return client.ContainerInspectResult{}, err
	}
	c, err := f.find(name)
	if err != nil {
		return client.ContainerInspectResult{}, err
	}
	return client.ContainerInspectResult{Container: container.InspectResponse{
		ID:     c.ID,
		Name:   "/" + name,
		Config: c.Config,
		State:  &container.State{Running: c.Running, ExitCode: c.ExitCode},
	}}, nil
}

func (f *Fake) ContainerUpdate(_ context.Context, name string, o client.ContainerUpdateOptions) (client.ContainerUpdateResult, error) {
	if err := f.call("ContainerUpdate"); err != nil {
		return client.ContainerUpdateResult{}, err
	}
	c, err := f.find(name)
	if err != nil {
		return client.ContainerUpdateResult{}, err
	}
	c.HostConfig.RestartPolicy = *o.RestartPolicy
	return client.ContainerUpdateResult{}, nil
}
