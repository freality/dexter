package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/state"
)

func TestRenderPlanEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderPlan(&buf, &diff.Plan{})
	if buf.String() != "No changes. Infrastructure matches configuration.\n" {
		t.Fatalf("renderPlan() = %q", buf.String())
	}
}

func TestRenderPlanWithChanges(t *testing.T) {
	plan := &diff.Plan{Changes: []diff.Change{
		{Action: diff.ActionCreate, Name: "whoami"},
		{Action: diff.ActionUpdate, Name: "media", Props: []diff.PropChange{{Key: "image"}, {Key: "ports"}}},
		{Action: diff.ActionDelete, Name: "old"},
	}}
	var buf bytes.Buffer
	renderPlan(&buf, plan)

	out := buf.String()
	for _, want := range []string{
		"\n  + whoami\n",
		"  ~ media (image, ports)\n",
		"  - old\n",
		"Plan: 1 to add, 1 to change, 1 to destroy.\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderPlan() = %q, want substring %q", out, want)
		}
	}
}

func TestRenderPlanMarksTaintedReplacement(t *testing.T) {
	plan := &diff.Plan{Changes: []diff.Change{
		{Action: diff.ActionDelete, Name: "whoami", Current: &state.Resource{Tainted: true}},
		{Action: diff.ActionCreate, Name: "whoami"},
	}}
	var buf bytes.Buffer
	renderPlan(&buf, plan)
	if !strings.Contains(buf.String(), "  - whoami (tainted: last apply failed)\n  + whoami\n") {
		t.Fatalf("renderPlan() = %q", buf.String())
	}
}
