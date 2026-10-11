package cli

import (
	"context"
	"encoding/json"

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

func (okAdapter) Create(context.Context, string, ir.Resource) (map[string]any, error) {
	return nil, nil
}
func (okAdapter) Update(context.Context, string, ir.Resource, state.Resource) (map[string]any, error) {
	return nil, nil
}
func (okAdapter) Delete(context.Context, string, state.Resource) error { return nil }

// fakeEngineDeps builds deps whose newEngine returns a real *engine.Engine
// wired to fakes, so it never boots esbuild/goja and its "<name>: done"
// progress lines land on out.
func fakeEngineDeps(doc *ir.Document, current *state.State) deps {
	registry := engine.NewRegistry()
	registry.Register("docker", okAdapter{})
	return engineDeps(doc, current, registry)
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

func failingEngineDeps(err error) deps {
	return deps{newEngine: func(dir string) (*engine.Engine, error) { return nil, err }}
}
