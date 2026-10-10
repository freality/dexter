// Package sdk embeds the pre-bundled @dex/iac JavaScript so it ships inside
// the dex binary; the user has no Node.js and no node_modules. Regenerate
// iac.js with `go run ./pkg/jsenv/sdk/gen` (wired into `task build:ts`).
package sdk

import _ "embed"

//go:embed iac.js
var Bundle string

// ModuleID is the bare specifier user code imports.
const ModuleID = "@dex/iac"
