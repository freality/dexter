// Package ir defines the JSON intermediate representation emitted by the
// @dex/iac SDK (SPEC.md §5.1) and validates it on the way into the engine.
package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/freality/dexter/pkg/engine/internal/maputil"
)

// NamePattern mirrors RESOURCE_NAME_PATTERN in packages/sdk/src/resource.ts.
var NamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Resource is one node in the IR, keyed by name in Document.Resources.
type Resource struct {
	Type       string         `json:"type"`
	Plugin     string         `json:"plugin"`
	Properties map[string]any `json:"properties"`
	DependsOn  []string       `json:"depends_on"`
}

// Document is the full synthesized IR for a project.
type Document struct {
	Project   string              `json:"project"`
	Resources map[string]Resource `json:"resources"`
}

// Names returns resource names in sorted order; map iteration is never used
// for anything observable.
func (d *Document) Names() []string {
	return maputil.SortedKeys(d.Resources)
}

// ValidationError reports the first structural problem found in a Document.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "invalid IR: " + e.Field + ": " + e.Reason
}

// Parse decodes IR JSON and validates it.
func Parse(data []byte) (*Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse IR: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Validate reports the first structural problem, or nil. Resources are
// walked in sorted-name order so the first error is deterministic.
func (d *Document) Validate() error {
	if d.Project == "" {
		return &ValidationError{Field: "project", Reason: "must not be empty"}
	}
	if d.Resources == nil {
		d.Resources = map[string]Resource{}
	}
	for _, name := range d.Names() {
		r := d.Resources[name]
		if !NamePattern.MatchString(name) {
			return &ValidationError{Field: fmt.Sprintf("resources[%q]", name), Reason: "invalid resource name"}
		}
		if r.Type == "" {
			return &ValidationError{Field: fmt.Sprintf("resources[%q].type", name), Reason: "must not be empty"}
		}
		if r.Plugin == "" {
			return &ValidationError{Field: fmt.Sprintf("resources[%q].plugin", name), Reason: "must not be empty"}
		}
		for i, dep := range r.DependsOn {
			if dep == name {
				return &ValidationError{
					Field:  fmt.Sprintf("resources[%q].depends_on[%d]", name, i),
					Reason: "self-dependency",
				}
			}
			if _, ok := d.Resources[dep]; !ok {
				return &ValidationError{
					Field:  fmt.Sprintf("resources[%q].depends_on[%d]", name, i),
					Reason: fmt.Sprintf("unknown resource %q", dep),
				}
			}
		}
		if r.Properties == nil {
			r.Properties = map[string]any{}
		}
		if r.DependsOn == nil {
			r.DependsOn = []string{}
		}
		d.Resources[name] = r
	}
	return nil
}
