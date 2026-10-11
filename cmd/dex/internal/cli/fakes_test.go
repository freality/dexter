package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

type fakeBundler struct {
	script string
	err    error
}

func (f *fakeBundler) Bundle(string, []string) (string, error) { return f.script, f.err }

type fakeEvaluator struct {
	data []byte
	err  error
}

func (f *fakeEvaluator) Evaluate(context.Context, string, map[string]string) ([]byte, error) {
	return f.data, f.err
}

type fakeStore struct {
	st      *state.State
	loadErr error
}

func (f *fakeStore) Load() (*state.State, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.st, nil
}
func (f *fakeStore) Save(s *state.State) error { f.st = s; return nil }

type noopLocker struct{}

func (noopLocker) Acquire() error { return nil }
func (noopLocker) Release() error { return nil }

type okAdapter struct{}

func (okAdapter) Create(context.Context, string, ir.Resource, func(engine.Event)) (map[string]any, error) {
	return nil, nil
}
func (okAdapter) Update(context.Context, string, ir.Resource, state.Resource, func(engine.Event)) (map[string]any, error) {
	return nil, nil
}
func (okAdapter) Delete(context.Context, string, state.Resource, func(engine.Event)) error {
	return nil
}

type nameFailAdapter struct {
	failCreate string
	failUpdate string
}

func (a nameFailAdapter) Create(_ context.Context, name string, _ ir.Resource, _ func(engine.Event)) (map[string]any, error) {
	if name == a.failCreate {
		return nil, errors.New("boom")
	}
	return nil, nil
}
func (a nameFailAdapter) Update(_ context.Context, name string, _ ir.Resource, _ state.Resource, _ func(engine.Event)) (map[string]any, error) {
	if name == a.failUpdate {
		return nil, errors.New("boom")
	}
	return nil, nil
}
func (nameFailAdapter) Delete(_ context.Context, _ string, _ state.Resource, _ func(engine.Event)) error {
	return nil
}

// fakeEngineDeps builds deps whose newEngine returns a real *engine.Engine
// wired to fakes, so it never boots esbuild/goja and its "<name>: done"
// progress lines land on out.
func fakeEngineDeps(doc *ir.Document, current *state.State) deps {
	reg := testRegistry(map[string]engine.Adapter{"docker": okAdapter{}})
	return engineDeps(doc, current, reg)
}

// testRegistry is a test helper: NewRegistry with a resolve that looks up
// the name in a caller-supplied map.
func testRegistry(adapters map[string]engine.Adapter) *engine.Registry {
	return engine.NewRegistry(func(plugin string) (engine.Adapter, error) {
		a, ok := adapters[plugin]
		if !ok {
			return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
		}
		return a, nil
	})
}

// engineDeps is fakeEngineDeps with the caller's adapter registry.
func engineDeps(doc *ir.Document, current *state.State, registry *engine.Registry) deps {
	var data []byte
	if doc != nil {
		b, _ := json.Marshal(doc)
		data = b
	}
	return deps{
		newEngine: func(dir string) (*engine.Engine, error) {
			return engine.New(engine.Options{
				Synth:    jsenv.NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: data}),
				Store:    &fakeStore{st: current},
				Lock:     noopLocker{},
				Registry: registry,
			}), nil
		},
	}
}

// eventsEngineDeps is engineDeps with an Events sink, so apply-output tests
// capture progress lines exactly as the production sink renders them.
func eventsEngineDeps(doc *ir.Document, current *state.State, registry *engine.Registry, sink func(engine.Event)) deps {
	var data []byte
	if doc != nil {
		b, _ := json.Marshal(doc)
		data = b
	}
	return deps{
		newEngine: func(dir string) (*engine.Engine, error) {
			return engine.New(engine.Options{
				Synth:    jsenv.NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: data}),
				Store:    &fakeStore{st: current},
				Lock:     noopLocker{},
				Registry: registry,
				Events:   sink,
			}), nil
		},
	}
}

func failingEngineDeps(err error) deps {
	return deps{newEngine: func(dir string) (*engine.Engine, error) { return nil, err }}
}
