// Package jsenv bundles a TypeScript entrypoint with esbuild and evaluates
// it in a goja VM to produce the JSON IR (SPEC.md §5.1).
package jsenv

import (
	"context"
	"fmt"

	"github.com/freality/dexter/pkg/engine/ir"
)

// Synthesizer turns a TypeScript entrypoint into a validated IR document.
type Synthesizer struct {
	bundler   Bundler
	evaluator Evaluator
}

// NewSynthesizer wires a Bundler and Evaluator into a Synthesizer.
func NewSynthesizer(b Bundler, e Evaluator) *Synthesizer {
	return &Synthesizer{bundler: b, evaluator: e}
}

// Synth bundles entrypoint, evaluates it with env exposed to the SDK's
// env() helper, and parses+validates the resulting IR.
func (s *Synthesizer) Synth(ctx context.Context, entrypoint string, env map[string]string) (*ir.Document, error) {
	script, err := s.bundler.Bundle(entrypoint)
	if err != nil {
		return nil, err
	}
	data, err := s.evaluator.Evaluate(ctx, script, env)
	if err != nil {
		return nil, err
	}
	doc, err := ir.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("synth: %w", err)
	}
	return doc, nil
}
