package jsenv

import (
	"fmt"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/freality/dexter/pkg/jsenv/sdk"
)

// Bundler transpiles and bundles a TypeScript entrypoint into a single
// script runnable by an Evaluator.
type Bundler interface {
	Bundle(entrypoint string) (string, error)
}

// EsbuildBundler bundles with esbuild, resolving the bare "@dex/iac"
// specifier to the SDK embedded in the binary.
type EsbuildBundler struct{}

// NewEsbuildBundler returns a Bundler backed by esbuild.
func NewEsbuildBundler() *EsbuildBundler {
	return &EsbuildBundler{}
}

// Bundle transpiles and bundles entrypoint into a single IIFE script. Any
// bare import other than "@dex/iac" stays unresolvable and surfaces as an
// esbuild error — correct, since no package manager exists on the user
// machine.
func (b *EsbuildBundler) Bundle(entrypoint string) (string, error) {
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{entrypoint},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		GlobalName:  "__dex_entry",
		Platform:    api.PlatformNeutral,
		Target:      api.ES2017,
		Plugins:     []api.Plugin{sdkPlugin()},
	})
	if len(result.Errors) > 0 {
		msg := result.Errors[0].Text
		if loc := result.Errors[0].Location; loc != nil {
			msg = fmt.Sprintf("%s:%d: %s", loc.File, loc.Line, msg)
		}
		return "", fmt.Errorf("bundle %s: %s", entrypoint, msg)
	}
	return string(result.OutputFiles[0].Contents), nil
}

// sdkPlugin resolves the bare "@dex/iac" specifier to the embedded SDK
// bundle instead of hitting the filesystem. The SDK module is a dependency
// of the entry module, so its top-level `globalThis.__dex_synth = …` side
// effect runs before user code.
func sdkPlugin() api.Plugin {
	return api.Plugin{
		Name: "dex-sdk",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: "^@dex/iac$"},
				func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					return api.OnResolveResult{Path: sdk.ModuleID, Namespace: "dex-sdk"}, nil
				})
			build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "dex-sdk"},
				func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					return api.OnLoadResult{Contents: &sdk.Bundle, Loader: api.LoaderJS}, nil
				})
		},
	}
}
