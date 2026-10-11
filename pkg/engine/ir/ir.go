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

// PluginNamePattern mirrors PLUGIN_NAME_PATTERN in packages/sdk/src/resource.ts.
// Plugins are binary names (dex-adapter-<name>), so only lowercase
// alphanumeric and hyphens are allowed. No dots or underscores.
var PluginNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// PluginSpecPattern validates plugin spec strings like
// github.com/<owner>/<repo>[@tag]. Matches the SDK's PLUGIN_SPEC_PATTERN.
var PluginSpecPattern = regexp.MustCompile(`^github\.com/[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*(@[^@]+)?$`)

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
	Plugins   []string            `json:"plugins"`
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

// validateProperties recursively walks props looking for $dex_type token maps.
// A token must be exactly {"$dex_type": "secret", "provider": "env", "key": <non-empty string>}
// with no extra keys. Any deviation is reported as a *ValidationError at path.
func validateProperties(props map[string]any, path string) *ValidationError {
	if _, ok := props["$dex_type"]; ok {
		// This is a secret token map. Validate its shape exactly.
		const wantFields = 3
		if len(props) != wantFields {
			return &ValidationError{Field: path, Reason: fmt.Sprintf("secret token must have exactly %d fields ($dex_type, provider, key)", wantFields)}
		}
		if dt, _ := props["$dex_type"].(string); dt != "secret" {
			return &ValidationError{Field: path + ".$dex_type", Reason: `must be "secret"`}
		}
		if p, _ := props["provider"].(string); p != "env" {
			return &ValidationError{Field: path + ".provider", Reason: `must be "env"`}
		}
		key, _ := props["key"].(string)
		if key == "" {
			return &ValidationError{Field: path + ".key", Reason: "must be a non-empty string"}
		}
		return nil
	}
	for k, v := range props {
		childPath := path + "." + k
		switch val := v.(type) {
		case map[string]any:
			if ve := validateProperties(val, childPath); ve != nil {
				return ve
			}
		case []any:
			for i, item := range val {
				if m, ok := item.(map[string]any); ok {
					if ve := validateProperties(m, fmt.Sprintf("%s[%d]", childPath, i)); ve != nil {
						return ve
					}
				}
			}
		}
	}
	return nil
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
	if d.Plugins == nil {
		d.Plugins = []string{}
	}
	for i, spec := range d.Plugins {
		if !PluginSpecPattern.MatchString(spec) {
			return &ValidationError{
				Field:  fmt.Sprintf("plugins[%d]", i),
				Reason: fmt.Sprintf("invalid plugin spec %q: must match github.com/<owner>/<repo>[@tag]", spec),
			}
		}
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
		if !PluginNamePattern.MatchString(r.Plugin) {
			return &ValidationError{Field: fmt.Sprintf("resources[%q].plugin", name), Reason: "invalid plugin name"}
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
		if ve := validateProperties(r.Properties, fmt.Sprintf("resources[%q].properties", name)); ve != nil {
			return ve
		}
	}
	return nil
}
