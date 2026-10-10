package ir

import (
	"errors"
	"strings"
	"testing"
)

func TestNamesSorted(t *testing.T) {
	d := &Document{Resources: map[string]Resource{
		"zeta":  {Type: "t", Plugin: "p"},
		"alpha": {Type: "t", Plugin: "p"},
		"mid":   {Type: "t", Plugin: "p"},
	}}
	got := d.Names()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

func TestParseSuccess(t *testing.T) {
	data := []byte(`{
		"project": "my-homelab",
		"resources": {
			"network": {"type": "network", "plugin": "docker", "properties": {}, "depends_on": []},
			"postgres": {"type": "container", "plugin": "docker", "properties": {"image": "postgres:15"}, "depends_on": ["network"]}
		}
	}`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Project != "my-homelab" {
		t.Fatalf("Project = %q", doc.Project)
	}
	if len(doc.Resources) != 2 {
		t.Fatalf("len(Resources) = %d", len(doc.Resources))
	}
	pg := doc.Resources["postgres"]
	if pg.Properties["image"] != "postgres:15" {
		t.Fatalf("properties not decoded: %+v", pg.Properties)
	}
	if len(pg.DependsOn) != 1 || pg.DependsOn[0] != "network" {
		t.Fatalf("depends_on = %v", pg.DependsOn)
	}
}

func TestParseMalformedJSON(t *testing.T) {
	_, err := Parse([]byte(`{not json`))
	if err == nil || !strings.Contains(err.Error(), "parse IR:") {
		t.Fatalf("Parse() error = %v, want wrapped parse error", err)
	}
}

func TestParseUnknownField(t *testing.T) {
	_, err := Parse([]byte(`{"project": "p", "resources": {}, "bogus": true}`))
	if err == nil || !strings.Contains(err.Error(), "parse IR:") {
		t.Fatalf("Parse() error = %v, want unknown field rejection", err)
	}
}

func TestParseValidationError(t *testing.T) {
	_, err := Parse([]byte(`{"project": "", "resources": {}}`))
	var ve *ValidationError
	if err == nil {
		t.Fatal("Parse() error = nil, want ValidationError")
	}
	if !errors.As(err, &ve) {
		t.Fatalf("Parse() error = %v (%T), want *ValidationError", err, err)
	}
	if ve.Field != "project" || ve.Reason != "must not be empty" {
		t.Fatalf("ValidationError = %+v", ve)
	}
	if ve.Error() != "invalid IR: project: must not be empty" {
		t.Fatalf("Error() = %q", ve.Error())
	}
}

func TestParseNilResourcesIsLegal(t *testing.T) {
	doc, err := Parse([]byte(`{"project": "p"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Resources == nil {
		t.Fatal("Resources not normalized to empty map")
	}
}

func TestValidateRows(t *testing.T) {
	cases := []struct {
		name       string
		doc        *Document
		wantField  string
		wantReason string
	}{
		{
			name:       "empty project",
			doc:        &Document{Project: ""},
			wantField:  "project",
			wantReason: "must not be empty",
		},
		{
			name: "invalid resource name",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"Bad Name": {Type: "t", Plugin: "p"},
			}},
			wantField:  `resources["Bad Name"]`,
			wantReason: "invalid resource name",
		},
		{
			name: "empty type",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "", Plugin: "p"},
			}},
			wantField:  `resources["a"].type`,
			wantReason: "must not be empty",
		},
		{
			name: "empty plugin",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: ""},
			}},
			wantField:  `resources["a"].plugin`,
			wantReason: "must not be empty",
		},
		{
			name: "self dependency",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "p", DependsOn: []string{"a"}},
			}},
			wantField:  `resources["a"].depends_on[0]`,
			wantReason: "self-dependency",
		},
		{
			name: "unknown dependency",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "p", DependsOn: []string{"ghost"}},
			}},
			wantField:  `resources["a"].depends_on[0]`,
			wantReason: `unknown resource "ghost"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.doc.Validate()
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
			}
			if ve.Field != c.wantField || ve.Reason != c.wantReason {
				t.Fatalf("Validate() = %+v, want field=%q reason=%q", ve, c.wantField, c.wantReason)
			}
		})
	}
}

func TestValidateNormalizesNilFields(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"a": {Type: "t", Plugin: "p", Properties: nil, DependsOn: nil},
	}}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	a := doc.Resources["a"]
	if a.Properties == nil {
		t.Fatal("Properties not normalized to empty map")
	}
	if a.DependsOn == nil {
		t.Fatal("DependsOn not normalized to empty slice")
	}
}

func TestValidateEmptyResourcesOK(t *testing.T) {
	doc := &Document{Project: "p"}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
