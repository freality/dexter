// Package jsenv bundles a TypeScript entrypoint with esbuild and evaluates
// it in a goja VM to produce the JSON IR (SPEC.md §5.1).
package jsenv

import (
	"context"
)

// Synthesizer turns a TypeScript entrypoint into raw IR JSON. Parsing and
// validating that JSON is the engine's job (SPEC.md §4 Step 2.1), not this
// TS-execution wrapper's: keeping it here would make pkg/jsenv depend on
// pkg/engine/ir while pkg/engine depends on pkg/jsenv, a loop back into the
// engine.
type Synthesizer struct {
	bundler   Bundler
	evaluator Evaluator
}

// NewSynthesizer wires a Bundler and Evaluator into a Synthesizer.
func NewSynthesizer(b Bundler, e Evaluator) *Synthesizer {
	return &Synthesizer{bundler: b, evaluator: e}
}

// Synth discovers dir's config files (dex.config.ts plus dex/services/**),
// bundles them, and evaluates the result with env exposed to the SDK's
// env() helper, returning the raw IR JSON bytes (SPEC.md §5.1).
func (s *Synthesizer) Synth(ctx context.Context, dir string, env map[string]string) ([]byte, error) {
	files, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	script, err := s.bundler.Bundle(dir, files)
	if err != nil {
		return nil, err
	}
	return s.evaluator.Evaluate(ctx, script, env)
}
