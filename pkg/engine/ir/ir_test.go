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
			name: "invalid plugin name with dot",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "my.plugin"},
			}},
			wantField:  `resources["a"].plugin`,
			wantReason: "invalid plugin name",
		},
		{
			name: "invalid plugin name with underscore",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "my_plugin"},
			}},
			wantField:  `resources["a"].plugin`,
			wantReason: "invalid plugin name",
		},
		{
			name: "invalid plugin name with uppercase",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "MyPlugin"},
			}},
			wantField:  `resources["a"].plugin`,
			wantReason: "invalid plugin name",
		},
		{
			name: "path-traversal plugin name",
			doc: &Document{Project: "p", Resources: map[string]Resource{
				"a": {Type: "t", Plugin: "../x"},
			}},
			wantField:  `resources["a"].plugin`,
			wantReason: "invalid plugin name",
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

// --- secret token validation --------------------------------------------------

func secretResource(properties map[string]any) Resource {
	return Resource{Type: "container", Plugin: "docker", Properties: properties, DependsOn: []string{}}
}

func TestValidateValidSecretToken(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"image": "img", "env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PASSWORD"},
		}}),
	}}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateSecretTokenExtraField(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": []any{
			map[string]any{"$dex_type": "secret", "provider": "env", "key": "x", "sha256": "abc"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if !strings.Contains(ve.Field, ".properties.env[0]") || !strings.Contains(ve.Reason, "exactly 3") {
		t.Fatalf("Validate() = %+v, want extra-field rejection in array", ve)
	}
}

// --- plugins field tests -----------------------------------------------------

func TestPluginsDefaultsToEmpty(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{}}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(doc.Plugins) != 0 {
		t.Fatalf("doc.Plugins = %v, want []", doc.Plugins)
	}
}

func TestPluginsParseRoundTrip(t *testing.T) {
	data := []byte(`{
		"project": "p",
		"plugins": ["github.com/freality/dexter@v1.0.0"],
		"resources": {}
	}`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(doc.Plugins) != 1 || doc.Plugins[0] != "github.com/freality/dexter@v1.0.0" {
		t.Fatalf("doc.Plugins = %v, want [github.com/freality/dexter@v1.0.0]", doc.Plugins)
	}
	if doc.Project != "p" {
		t.Fatalf("doc.Project = %q, want p", doc.Project)
	}
}

func TestPluginsParseEmpty(t *testing.T) {
	data := []byte(`{
		"project": "p",
		"plugins": [],
		"resources": {}
	}`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(doc.Plugins) != 0 {
		t.Fatalf("doc.Plugins = %v, want []", doc.Plugins)
	}
}

func TestPluginsParseOmitsField(t *testing.T) {
	// Old IR without "plugins" field should default to [].
	data := []byte(`{
		"project": "p",
		"resources": {
			"svc": {"type": "container", "plugin": "docker", "properties": {}, "depends_on": []}
	}
	}`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(doc.Plugins) != 0 {
		t.Fatalf("doc.Plugins = %v, want []", doc.Plugins)
	}
}

func TestPluginsValidateRejectsMalformed(t *testing.T) {
	doc := &Document{Project: "p", Plugins: []string{"not-a-url"}, Resources: map[string]Resource{}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if ve.Field != `plugins[0]` || !strings.Contains(ve.Reason, "invalid plugin spec") {
		t.Fatalf("Validate() = %+v, want plugins[0] rejection", ve)
	}
}

func TestPluginsValidateAcceptsValid(t *testing.T) {
	doc := &Document{Project: "p", Plugins: []string{
		"github.com/owner/repo",
		"github.com/owner/repo@v1.0.0",
		"github.com/owner/repo-name",
	}, Resources: map[string]Resource{}}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
func TestValidateSecretTokenWrongProvider(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "vault", "key": "x"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if ve.Field != `resources["svc"].properties.env.PASSWORD.provider` || ve.Reason != `must be "env"` {
		t.Fatalf("Validate() = %+v, want provider rejection", ve)
	}
}

func TestValidateSecretTokenEmptyKey(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": ""},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if ve.Field != `resources["svc"].properties.env.PASSWORD.key` || ve.Reason != "must be a non-empty string" {
		t.Fatalf("Validate() = %+v, want empty-key rejection", ve)
	}
}

func TestValidateSecretTokenMissingKey(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if ve.Field != `resources["svc"].properties.env.PASSWORD` || !strings.Contains(ve.Reason, "exactly 3") {
		t.Fatalf("Validate() = %+v, want missing-key rejection", ve)
	}
}

func TestValidateSecretTokenNestedInArray(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": []any{
			map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
		}}),
	}}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateSecretTokenSpoofedHash(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "x", "sha256": "spoof"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if !strings.Contains(ve.Reason, "exactly 3") {
		t.Fatalf("Validate() = %+v, want extra-field rejection", ve)
	}
}
func TestValidateSecretWrongType(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": map[string]any{
			"PASSWORD": map[string]any{"$dex_type": "not_secret", "provider": "env", "key": "x"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if !strings.Contains(ve.Field, ".properties.env.PASSWORD.$dex_type") || !strings.Contains(ve.Reason, `must be "secret"`) {
		t.Fatalf("Validate() = %+v, want wrong-dex-type rejection", ve)
	}
}

func TestValidateSecretInArrayInvalid(t *testing.T) {
	doc := &Document{Project: "p", Resources: map[string]Resource{
		"svc": secretResource(map[string]any{"env": []any{
			map[string]any{"$dex_type": "secret", "provider": "env", "key": "x", "sha256": "abc"},
		}}),
	}}
	err := doc.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() error = %v (%T), want *ValidationError", err, err)
	}
	if !strings.Contains(ve.Field, ".properties.env[0]") || !strings.Contains(ve.Reason, "exactly 3") {
		t.Fatalf("Validate() = %+v, want extra-field rejection in array", ve)
	}
}
