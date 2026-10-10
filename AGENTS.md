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
- **Phase 1 exception:** the Docker adapter (`adapters/docker`) runs in-process, called through a small `engine.Adapter` wrapper in `cmd/dex/internal/cli`. It already imports nothing from this repo, so Phase 2 can wrap it in a gRPC binary unchanged.

The IR and `proto/adapter.proto` are the two contracts. Changing either is one change across every side: SDK emitter, engine parser, adapters, and the SPEC section.

## Userland artifacts

Everything the engine writes into a user's project lives under `.dex/` (layout: SPEC §3). The only exceptions are the files `dex init` scaffolds for the user (`dex.config.ts`, `.env`, `dex/`).

## Toolchain

The Go and Node ecosystems stay separate, split by directory:

- TypeScript: `pnpm` only (version pinned in `package.json`), workspace `packages/*`.
- Go: the modules listed in `go.work`.
- `task` is the bridge. Use `task build` for a full build (`build:go` depends on `build:ts`, since the Go binary embeds the built SDK bundle); `task check` is the full gate (CI and the pre-commit hook from `task setup:hooks` run it); `task --list` shows the rest.

## License

MIT. New dependencies must have an MIT-compatible license.

## Agent skills

### Issue tracker

GitHub Issues on `freality/dexter`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `GLOSSARY.md` + `docs/adr/`, created on demand. See `docs/agents/domain.md`.
