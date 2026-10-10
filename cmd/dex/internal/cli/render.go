package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/freality/dexter/pkg/engine/diff"
)

// renderPlan prints a human plan summary shared by `dex plan` and
// `dex apply`.
func renderPlan(w io.Writer, p *diff.Plan) {
	if p.Empty() {
		fmt.Fprintln(w, "No changes. Infrastructure matches configuration.")
		return
	}

	fmt.Fprintln(w)
	for _, c := range p.Changes {
		switch c.Action {
		case diff.ActionCreate:
			fmt.Fprintf(w, "  + %s\n", c.Name)
		case diff.ActionUpdate:
			keys := make([]string, len(c.Props))
			for i, pc := range c.Props {
				keys[i] = pc.Key
			}
			fmt.Fprintf(w, "  ~ %s (%s)\n", c.Name, strings.Join(keys, ", "))
		case diff.ActionDelete:
			fmt.Fprintf(w, "  - %s\n", c.Name)
		}
	}
	fmt.Fprintln(w)

	add, change, destroy := p.Counts()
	fmt.Fprintf(w, "Plan: %d to add, %d to change, %d to destroy.\n", add, change, destroy)
}
