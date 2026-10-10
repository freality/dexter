# AGENTS.md

Dexter (`dex`): homelab IaC + control plane. Users write TypeScript; a single Go binary synthesizes, plans, and applies it. No Node.js on the user's machine.

## Read first

- `SPEC.md` — authoritative for architecture, the JSON IR (§5.1), the adapter gRPC contract (§5.2), and coding directives (§6). Read §5–§6 before writing Go or TS. When docs disagree, SPEC wins.
- `ROADMAP.md` — phased checklist. Tick the box when you land an item.
- `PROJECT.md` — vision and principles; read when a design choice is ambiguous.

## System boundaries

```
@dex/iac (TS SDK) ──JSON IR──▶ dex (Go engine: DAG, state, diff) ──gRPC──▶ dex-adapter-* (Go plugins)
```

- **SDK** (`packages/`): builder functions (`service`, `volume`, `network`, …) that emit the IR. It runs inside `goja` after `esbuild` bundles it, so it is pure ECMAScript: no Node built-ins, no Node-dependent packages.
- **Engine** (`cmd/dex`, `pkg/`): parses IR, builds the DAG, diffs against state, drives adapters. It is a dumb orchestrator; target-specific logic belongs in an adapter.
- **Adapters** (`cmd/dex-adapter-<target>`): standalone binaries that speak only the gRPC contract. Third parties build them from the public plugin package alone, so adapters import `pkg/plugin` and never `pkg/engine`.

The IR and `proto/adapter.proto` are the two contracts. Changing either is one change across every side: SDK emitter, engine parser, adapters, and the SPEC section.

## Userland artifacts

Everything the engine writes into a user's project lives under `.dex/` at the project root: `state.json`, `state.json.lock`, `plugins/` (binaries fetched by `dex plugin add github.com/<user>/<adapter>` from GitHub Releases for the host OS/arch), `generated/` (`.d.ts` files). The only exceptions are the files `dex init` scaffolds for the user (`dex.config.ts`, `.env`, `dex/`).

## Toolchain

The Go and Node ecosystems stay separate, split by directory:

- TypeScript: `pnpm` only (version pinned in `package.json`), workspace `packages/*`.
- Go: modules under `cmd/` and `pkg/`, registered in `go.work`.
- `task` is the bridge. Use `task build` for a full build (TS and Go run concurrently); `task --list` shows the rest.

## License

MIT. New dependencies must have an MIT-compatible license.
