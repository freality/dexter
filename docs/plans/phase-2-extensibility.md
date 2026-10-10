# Phase 2 plan: Extensibility & Organization (V1)

**Goal (ROADMAP Phase 2):** make `dex` usable for real homelabs. That means configs split across files, secrets that never reach IR or state, adapters shipped as separate gRPC binaries installable with `dex plugin add`, custom commands, and a TUI showing apply progress.

**Status:** verified against the code at `7f14b46`. Decisions marked **P#** are *proposed* and need confirming in a `/grill-with-docs` session before `/to-tickets`, the same way Phase 1's D1–D17 were confirmed.

## Phase 1 verification

`task check` is green at `7f14b46`: lint, vet, shadow, Go tests at 100% coverage on every module, and SDK tests at 100%. Every Phase 1 checkbox matches the code:

| ROADMAP item | Where it lives |
|---|---|
| SDK `defineDexConfig`/`service`/`env`/`synth` | `packages/sdk/src/scope.ts`, `index.ts` (also `volume`, `network`) |
| Cobra `init`/`plan`/`apply` | `cmd/dex/internal/cli/{init,plan,apply}.go` |
| esbuild in-memory transpile | `pkg/jsenv/bundler.go` `EsbuildBundler.Bundle` (single file entry, `sdkPlugin` resolves `@dex/iac`) |
| goja evaluation | `pkg/jsenv/evaluator.go` `GojaEvaluator.Evaluate` (10s budget, `__dex_env` in, `__dex_synth()` out) |
| DAG | `pkg/engine/dag/dag.go`; used only by `diff.Compute` to order changes |
| State + lock | `pkg/engine/state/{file,lock}.go` |
| Diff | `pkg/engine/diff/diff.go` (tainted → replace, deletes reverse-ordered) |
| Docker adapter | `adapters/docker` (own module), bridged by `cmd/dex/internal/cli/docker.go` `dockerAdapter` → `engine.Adapter` |

These facts matter for Phase 2:

- `engine.Apply` runs `plan.Changes` **serially** and reports through `Options.Out io.Writer` (`"%s: done"`). There is no event bus and no concurrency.
- `engine.Adapter` is `Create`/`Update`/`Delete`. `Update` receives the current `state.Resource`, and `docker.Adapter.Update` needs the prior properties for its restart-only fast path (Phase 1 D5).
- The env passed to `env()` comes from process env only (`root.go` `envMap`). `.env` is scaffolded by `dex init` but never read.
- `dex init` already creates `dex/services/.gitkeep`, but nothing reads that directory.
- `ir.Validate` never checks the `plugin` field's shape. Once plugins become file paths (W3), that field is a path-traversal vector.
- There's no signal handling: `root.Execute()` runs with a background context. Ctrl-C can't cancel adapter calls, and once plugins exist it would orphan their processes.
- The coverage gate (`scripts/check-coverage.sh`) excludes only `cmd/dex/main.go` and `pkg/jsenv/sdk/gen/main.go`. Generated protobuf code and new `main.go` files have to be added to that filter.

## Contradictions found

These are places where the docs disagree with each other or with the code. AGENTS.md says SPEC wins, and every fix below updates SPEC in the same change.

| # | Conflict | Proposed resolution |
|---|---|---|
| C1 | SPEC §5.2 proto has `Configure`/`Read`/`Apply`/`Destroy`, and `ApplyRequest` carries only `resource_id` + `properties_json`. The engine's seam is Create/Update/Delete with the prior state passed in, and Docker's `Update` needs `type` plus prior properties. | P5 |
| C2 | SPEC §3 says `.env` is "never serialized to IR". But `env()` returns plaintext into properties, and the scaffold (`env("TZ", "UTC")` plus `.env` containing `TZ=UTC`) implies `.env` feeds `env()`. | P3 |
| C3 | AGENTS.md puts adapter binaries at `cmd/dex-adapter-<target>`; SPEC §2 puts them under `adapters/docker`. Phase 1 D2 left this open. | P6 |
| C4 | SPEC §4 Step 3.1 says "iterates the DAG concurrently", and the ROADMAP TUI item says "concurrent deployment progress bars". The engine is serial, and concurrent apply isn't a ROADMAP item. | P10 |
| C5 | PROJECT.md's "internal Event Bus" doesn't exist. The TUI item assumes it does. | P11 |
| C6 | AGENTS.md: "new dependencies must have an MIT-compatible license". SPEC mandates `hashicorp/go-plugin`, which is **MPL-2.0** (checked on the GitHub API; latest v1.8.0). MPL-2.0 is file-level copyleft: using it unmodified as a dependency is generally considered compatible, but it isn't permissive. | Needs an explicit sign-off (P4) |
| C7 | PROJECT.md: `dex plugin add github.com/username/dex-adapter-name`, i.e. one repo per adapter. The first-party Docker adapter is released from the `freality/dexter` monorepo. | P8 |
| C8 | `.dex/` is gitignored, so plugins installed into `.dex/plugins/` don't survive a fresh clone. PROJECT.md says `dex.config.ts` does "plugin setup", but `DexConfig` has only `project`. | P9 |
| C9 | PROJECT.md still says `.dex-state.json`; the code uses `.dex/state.json`. | Fix the doc |

## Proposed decisions

| # | Decision |
|---|---|
| P1 | **Discovery:** the entry imports `dex.config.ts` first, then every `dex/services/**/*.ts` in sorted slash-path order. `*.d.ts` and dot-prefixed directories are skipped, and a missing `dex/services/` is fine. Order is cosmetic: the registry is name-keyed and `buildIR` sorts. The import order only makes the "first error" deterministic. |
| P2 | **Virtual entrypoint:** esbuild `BuildOptions.Stdin{Contents, ResolveDir: dir, Sourcefile: "dex:entry", Loader: LoaderTS}`, containing one `import "./<path>";` line per file. **Prototyped:** two service files plus a nested cross-file import bundle into one IR, the shared module evaluates once, and all files share one `@dex/iac` registry. The scaffold moves `whoami` into `dex/services/whoami.ts` and leaves `dex.config.ts` holding only `defineDexConfig`, matching SPEC §3. |
| P3 | **Env and `.env`:** load `<dir>/.env` with a minimal parser: `KEY=VALUE`, `#` comments, an optional `export ` prefix, and one level of matching quotes stripped. No interpolation, no multiline. Process env overrides `.env`. That one map feeds both `env()` and `secret()`. `env()` stays deliberately plaintext, and SPEC §3 is reworded to "`.env` values reach IR only through `env()`; `secret()` values never do". |
| P4 | **go-plugin:** adopt it per SPEC, with an MPL-2.0 sign-off recorded in AGENTS.md's License section. Transitive dependencies to license-check: `google.golang.org/grpc` (Apache-2.0), `google.golang.org/protobuf` (BSD-3), `hashicorp/go-hclog` (MIT). |
| P5 | **Proto (Phase 2 subset):** `Apply(ApplyRequest) returns (stream ApplyResponse)` and `Destroy(DestroyRequest) returns (stream ApplyResponse)`. `ApplyRequest{resource_id, type, properties_json, prior_properties_json}`, where an empty prior means create. `DestroyRequest{resource_id, type, properties_json, outputs_json}`. Keep SPEC's `ApplyResponse` (`LOG`/`PROGRESS`/`SUCCESS`/`ERROR`, `message`, `new_state_json`); `PROGRESS` puts an integer percent in `message`. **Drop `Configure` and `Read` for now:** nothing consumes them (no plugin-config block in IR; no refresh, per Phase 1 D9). SPEC §5.2 records them as added when a consumer lands. |
| P6 | **Binary location:** `adapters/docker/cmd/dex-adapter-docker/main.go` inside the existing `adapters/docker` module, so moby stays out of the `cmd/dex` module. AGENTS.md changes `cmd/dex-adapter-<target>` to `adapters/<target>/cmd/dex-adapter-<target>`. |
| P7 | **Plugin lookup:** `engine.Registry` resolves lazily, so only plugins named by a planned change get started, and `dex plan` stays offline with no plugin processes. It looks in `<dir>/.dex/plugins/dex-adapter-<name>` first, then `exec.LookPath("dex-adapter-<name>")`. If neither exists, the error is `plugin "<name>" not installed (run: dex plugin add <repo>)`. The in-process Docker path is **deleted** (clean cutover). `ir.Validate` rejects any `plugin` not matching `^[a-z0-9][a-z0-9-]*$`, and the SDK mirrors this in `resource.ts`. |
| P8 | **Release asset convention:** a release contains raw binaries named `dex-adapter-<name>_<goos>_<goarch>` plus a `checksums.txt` in `sha256sum` format. `dex plugin add <github.com/owner/repo>[@tag]` installs every matching adapter asset for the host OS/arch, so one repo can ship many adapters. It fails if no asset matches or `checksums.txt` is missing or doesn't match. |
| P9 | **Reproducibility deferred:** `dex plugin add` writes only `.dex/plugins/`. A declared plugin list in `defineDexConfig` (with `dex plugin install` reading it) is noted as the upgrade path, to be built when a second machine needs it. |
| P10 | **Concurrent apply is added as an explicit Phase 2 item** ahead of the TUI. Without it, "concurrent progress bars" render one bar at a time. The alternative is a TUI over serial apply, with concurrency deferred. |
| P11 | **Event bus = a callback.** `engine.Options.Events func(Event)` replaces `Options.Out`. A real fan-out bus waits for Phase 3, when the daemon has multiple subscribers. |
| P12 | **Secret hashing:** follow SPEC (plain SHA-256 hex). Risk: a low-entropy secret's unsalted hash in `state.json` can be guessed offline. The alternative is HMAC-SHA256 with a random per-project key in `.dex/secret.key`, which costs one file. Needs a decision. |
| P13 | **Custom Commands Context:** see W5. This is the least-specified item: no doc defines `Context`. It needs its own grilling round before ticketing. |

## Work order

```mermaid
graph LR
  W1[W1 Auto-discovery + virtual entrypoint]
  W2[W2 Secrets]
  W3a[W3a proto + pkg/plugin]
  W3b[W3b dex-adapter-docker + lazy registry]
  W3c[W3c dex plugin add + release workflow]
  W4a[W4a Event callback]
  W4b[W4b Concurrent apply]
  W4c[W4c TUI]
  W5[W5 Custom commands]
  W3a --> W3b --> W3c
  W3a --> W4a
  W4a --> W4b --> W4c
  W1 --> W5
  W2 --> W5
```

W1, W2 and W3a have no blockers and can start in parallel. W2 injects secrets at the `engine.Adapter` call site, so it doesn't wait for gRPC; the gRPC bridge inherits the resolved properties.

## W1: Modular architecture (Auto-Discovery, Virtual Entrypoint)

1. `pkg/jsenv/discover.go`: `func Discover(dir string) ([]string, error)` returns `dex.config.ts` (an error if missing: `no dex.config.ts in %s (run "dex init")`) followed by the P1 file set, via `filepath.WalkDir`, as slash paths relative to `dir`.
2. `pkg/jsenv/bundler.go`: change `Bundler.Bundle(entrypoint string)` to `Bundle(dir string, files []string)`. Build the `Stdin` entry per P2, quoting each path with `strconv.Quote`. Error locations of `dex:entry` print as-is. Keep `sdkPlugin` unchanged.
3. `pkg/jsenv/jsenv.go`: change `Synthesizer.Synth(ctx, entrypoint, env)` to `Synth(ctx, dir, env)`, which calls `Discover` then `Bundle`. `engine.Engine.Plan(ctx, entrypoint)` becomes `Plan(ctx, dir)`.
4. `cmd/dex/internal/cli`: delete `entrypointPath` (Discover now owns the missing-config error) and pass `*dir` in `plan.go`/`apply.go`. `scaffold.go`: `dex.config.ts` holds only `defineDexConfig`, and a new `dex/services/whoami.ts` replaces the `.gitkeep`.
5. Update the `e2e_test.go` `config` helper so it writes services under `dex/services/`.

**Check:** `go test ./pkg/jsenv/... ./pkg/engine/... ./cmd/dex/...`. New jsenv cases over a `t.TempDir()` project: a nested `dex/services/a/b.ts`, a cross-file `import { net } from "./net"`, an ignored `.d.ts`, a missing services dir, a duplicate resource across two files surfacing `[DUPLICATE_RESOURCE]`, a missing `dex.config.ts`, and a syntax error reported with the right file and line. Smoke: run `bin/dex init && bin/dex plan` in a temp dir and expect `+ whoami`.

## W2: Secrets management

1. **SDK** `packages/sdk/src/secret.ts`: `secret(key: string): SecretRef` returns a frozen `{ $dex_type: "secret", provider: "env", key }`, and an empty or non-string key throws `DexError("INVALID_SECRET")`. Add `SecretRef` to `types.ts`, `"INVALID_SECRET"` to `errors.ts`, and wire it through `scope.ts` and `index.ts`. It doesn't check presence: resolving the key is the engine's job. `sanitizeProperties` already accepts the plain object.
2. **`.env`** `cmd/dex/internal/cli/dotenv.go`: `loadEnv(dir string) (map[string]string, error)` implements P3. `newProductionEngine` replaces `envMap()` with it. A malformed line errors with `.env:<line>`.
3. **IR validation** `pkg/engine/ir/ir.go`: walk properties. Any object with `$dex_type` must be exactly `{ $dex_type: "secret", provider: "env", key: <non-empty string> }`. Anything else, including a user-supplied `sha256`, is a `ValidationError`.
4. **Plan-time hashing** `pkg/engine/secrets.go`: `hashSecrets(doc *ir.Document, env map[string]string) error` adds `"sha256": hex(sha256(value))` to every token, and errors with `resource %q: secret %q is not set (.env or environment)` when the key is missing. `Engine.Plan` calls it after `ir.Parse` and before `diff.Compute`. The diff and state then compare and store token+hash, so a rotated secret shows up as `~ name (env)` and plaintext never reaches the plan or state.
5. **Apply-time injection**: `Engine.applyChange` passes the adapter a deep copy of `Desired.Properties`, with each token swapped for its plaintext (`resolveSecrets`). For `Update`, tokens in `cur.Properties` whose `sha256` matches the current value are resolved too, so Docker's `sameExceptRestart` fast path still works. A mismatched token is left as-is, and Docker's `parseSpec(cur)` fails into the full-replace path, which is correct because the value changed. State keeps saving `change.Desired.Properties` (hashed).
6. **Leak guard**: before persisting, `applyChange` fails the change if any string in the adapter's returned `outputs` equals a secret value resolved for that change. That way a careless adapter can't write plaintext into `state.json`.
7. SPEC §5.1/§6: document the `sha256` field, the "outputs MUST NOT echo secrets" adapter rule, and the P3 wording. README: secrets section.

**Check:** engine tests with a fake adapter. The adapter receives plaintext; `state.json` bytes don't contain it but do contain the hash; changing the value plans an update; a missing key fails `Plan`; the leak guard fires; the restart-only update with a secret in `env` takes the fast path through `docker.NewWithAPI(fake)`. SDK vitest covers `secret()`. Smoke: `.env` with `DB_PASSWORD=hunter2`, a service using `env: { P: secret("DB_PASSWORD") }`, apply. Then `docker inspect` shows the value, `grep -c hunter2 .dex/state.json` prints `0`, and editing `.env` then running `dex plan` shows `~ <name> (env)`.

## W3: Plugin ecosystem

### W3a: gRPC architecture

1. `proto/adapter.proto` per P5 (`package dex.plugin`, `go_package = "github.com/freality/dexter/pkg/plugin/pb"`). Generate with `buf` driven through `go run` (`buf.gen.yaml` using `protoc-gen-go` and `protoc-gen-go-grpc` pinned via `go run …@vX`), so no system `protoc` is needed. Add a `task gen:proto`, commit `pkg/plugin/pb/*.pb.go`, and have CI run `task gen:proto && git diff --exit-code`.
2. `pkg/plugin` (public; adapters import only this, per AGENTS.md):
   - `Handshake` (`goplugin.HandshakeConfig{ProtocolVersion: 1, MagicCookieKey: "DEX_PLUGIN", MagicCookieValue: …}`).
   - `type Adapter interface { Apply(ctx, Request, Reporter) (map[string]any, error); Destroy(ctx, Request, Reporter) error }`, with `Request{Name, Type string; Properties, Prior, Outputs map[string]any}` and `Reporter{Log(string); Progress(int)}`. These are Go-native types, so authors never touch JSON or pb.
   - `Serve(a Adapter)` (the binary side) and `Open(path string, logOut io.Writer) (Adapter, func(), error)` (the host side: `goplugin.NewClient` with `AllowedProtocols: gRPC`, `Logger` at Warn to `logOut`, and a kill func).
   - Stream contract: exactly one terminal `SUCCESS`/`ERROR`. A stream that ends without one is an error (`plugin %q: stream ended without result`).
3. `scripts/check-coverage.sh`: exclude `/pkg/plugin/pb/`.

**Check:** `go test ./pkg/plugin/...` runs a real gRPC round-trip in-process (`goplugin.TestPluginGRPCConn`) and covers: a create, an update with prior, a destroy, LOG/PROGRESS reaching the Reporter, an ERROR becoming a Go error, a missing terminal message, and context cancel reaching the server's ctx. 100% gate, excluding `pb`.

### W3b: Extracted Docker adapter

1. `adapters/docker/plugin.go`: `type Plugin struct{ *Adapter }` implements `plugin.Adapter`. `Apply` with an empty `Prior` calls `Create(ctx, name, typ, props)`; otherwise it calls `Update(ctx, name, props, prior)`. `Destroy` calls `Delete`. `docker.Adapter` itself stays unchanged.
2. `adapters/docker/cmd/dex-adapter-docker/main.go`: `plugin.Serve(docker.Plugin{must(docker.New())})`. Add this path to the coverage filter. `adapters/docker/go.mod` requires `github.com/freality/dexter/pkg` (resolved locally through `go.work`).
3. `pkg/engine/adapter.go`: change `NewRegistry()` to `NewRegistry(resolve func(plugin string) (Adapter, error))` with a per-name cache, and delete `Register` (one production caller plus its tests). `Get` calls `resolve` once per plugin.
4. `cmd/dex/internal/cli`: **delete `docker.go`** and the `adapters/docker` import, so moby leaves the `cmd/dex` module (run `go mod tidy`). New `plugins.go`: a `resolvePlugin(dir)` implementing P7 lookup via `plugin.Open`, and a `pluginAdapter` bridging `engine.Adapter` to `plugin.Adapter` (Create → prior nil; Update → prior `cur.Properties`; Delete → `cur.Properties` plus `cur.Outputs`).
5. `cmd/dex/internal/cli/root.go` `Main`: `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`, `root.ExecuteContext(ctx)`, `defer goplugin.CleanupClients()`.
6. `ir.Validate` and the SDK `resource.ts` add the plugin-name pattern (P7).
7. Taskfile `build:go`: also `go build -o bin/dex-adapter-docker ./adapters/docker/cmd/dex-adapter-docker`. Update the e2e test (`//go:build docker`) to put that binary on `PATH`.

**Check:** CLI tests resolve a fake through an injected `resolve` (no subprocess): an unknown plugin gives the "not installed" error, `../x` is rejected by IR validation, and `dex plan` never calls `resolve`. Smoke: `task build`, then rerun the Phase 1 smoke table (`docs/plans/phase-1-docker-adapter.md` step 7) with `bin/` on `PATH`. Every row behaves the same. A `ps` after apply shows no leftover `dex-adapter-docker`, and Ctrl-C during an image pull exits non-zero with the resource tainted.

### W3c: Plugin distribution

1. `cmd/dex/internal/cli/plugin.go`: `dex plugin add <github.com/owner/repo>[@tag]`.
   - Release metadata: `GET {base}/repos/{o}/{r}/releases/latest` or `…/releases/tags/{tag}`, with `Accept: application/vnd.github+json` and an optional `Authorization: Bearer $GITHUB_TOKEN`. `base` is injectable for tests.
   - Pick assets per P8 for `runtime.GOOS`/`GOARCH`, download `checksums.txt`, and verify SHA-256 of each binary **before** it gets the executable bit.
   - Write `.dex/plugins/dex-adapter-<name>.tmp` at `0o755`, then `os.Rename`. Print `installed dex-adapter-<name> <tag>`.
   - Errors: a bad spec, a missing release, no matching asset, a missing or mismatched checksum, and a non-200 status each get a distinct message.
2. `.github/workflows/release.yml` on `v*` tags: `task build:ts`, then `CGO_ENABLED=0` builds for `{linux,darwin}×{amd64,arm64}` of `dex_<os>_<arch>` and `dex-adapter-docker_<os>_<arch>`, then `sha256sum * > checksums.txt`, then `gh release create`.
3. README: install flow. `dex init` prints `run: dex plugin add github.com/freality/dexter`.

**Check:** `httptest.Server` cases: latest versus a tag, multiple adapters in one release, checksum mismatch (no file left behind), no matching asset, and the token header being sent. Smoke: after the first tagged release, run `dex plugin add github.com/freality/dexter` in a temp project and expect `.dex/plugins/dex-adapter-docker` to exist and `dex apply --yes` to deploy whoami.

## W4: Observability

### W4a: Event callback (P11)

`pkg/engine/event.go`: `type Event struct{ Name string; Action diff.Action; Kind EventKind; Message string; Percent int }`, where `Kind` is one of `Started|Log|Progress|Done|Failed`. `Options.Events func(Event)` replaces `Options.Out`. `engine.Adapter` methods gain a `report func(Event)` parameter (only the Kind/Message/Percent fields are used), and `pluginAdapter` forwards `plugin.Reporter` calls into it. The CLI's default sink prints the current `"<name>: done"` lines, so output is unchanged.

**Check:** engine tests assert the event sequence for a create success, an update failure (`Failed` with the error), and adapter `Log` passthrough.

### W4b: Concurrent apply (P10)

In `Engine.Apply`, schedule `plan.Changes` as a graph:

- A create or update of X waits on the changes for each of X's `DependsOn`.
- The create half of a replace pair waits on its delete.
- Pure deletes wait on every create and update (this keeps today's "deletes last"), and on deletes of resources that depend on them.

Use a fixed worker limit (`const maxParallel = 4`, with a `ponytail:` comment and a `--parallelism` flag as the upgrade path). State mutation and `store.Save` go behind one mutex, and every attempt is still saved. On the first failure: launch nothing new, wait for in-flight work, and return `errors.Join` of the failures.

**Check:** engine tests with a blocking fake adapter prove that two independent creates overlap, a dependent waits, a failure stops new launches while in-flight work finishes and gets saved, and deletes run after creates. Add `-race` to `test:go` for `./pkg/engine/...`.

### W4c: TUI

`cmd/dex/internal/cli/tui.go`: a `charmbracelet/bubbletea` model (MIT; `bubbles` spinner and progress, MIT) fed by `Events` through `p.Send`. Each resource gets one row: spinner, action, last log line, and a progress bar when `Percent > 0`. Docker's `ensureImage` stops discarding the pull stream and reports aggregate layer percent. If stdout isn't a TTY (`golang.org/x/term`), the plain-line sink is used. The confirmation prompt runs before the TUI starts.

**Check:** unit-test the model's `Update`/`View` directly for event → row state and the final summary, plus the non-TTY fallback. 100% gate. Smoke: in a real terminal, `dex apply --yes` with three independent services shows three concurrent rows; `dex apply --yes | cat` prints plain lines.

## W5: Custom commands (needs P13 grilling first)

Strawman to grill against:

- SDK: `defineCommand(name, { description?, run(ctx) })` registers into a command registry separate from resources, so synth output is unchanged. Commands live in any discovered file, plus `dex/commands/**/*.ts` (PROJECT.md layout) added to `Discover`.
- `ctx`: `args: string[]`, `env(key)`, `resources` (read-only state: properties with secret tokens as-is, plus outputs), and `exec(argv: string[]): number`, a host process with stdio attached, synchronous from goja's point of view.
- `dex run <cmd> [args…]`: bundle with W1, evaluate with no 10s budget (`NewGojaEvaluatorWithTimeout`), cancel on SIGINT via ctx, call `__dex_commands[cmd].run(ctx)`, and exit with the command's result. Plain `dex run` lists the commands.

Open questions: Is host `exec` acceptable, given that every `ctx` capability is a Go host function and therefore a security surface? Does exec inside a container need an adapter `Exec` RPC (a proto change)? Should `secret()` resolve inside commands?

**Check (once decided):** goja tests calling a registered command with a fake exec; CLI test for `dex run` unknown/list/exit-code; smoke `dex run hello` against a real script.

## Risks

- **go-plugin process lifecycle:** without `CleanupClients` and signal handling (W3b step 5), a crash or Ctrl-C leaves orphan adapter processes holding the Docker socket.
- **Third-party binaries run with the user's privileges.** Checksums only prove the download matches the release, not that the release is trustworthy. Signature verification (cosign/minisign) is the upgrade path; README must state the trust model.
- **`pkg` module versioning:** adapters outside this repo need a tagged `github.com/freality/dexter/pkg`. Importing `pkg/plugin` also pulls esbuild and goja into their `go.sum`, though not their binary. Splitting `pkg/plugin` into its own module is the fix if that bothers anyone.
- **Plaintext exposure surfaces** that remain after W2: adapter error messages, adapter logs streamed to the TUI, and `docker inspect` on the host. Only state and plan are guaranteed clean.
- **Concurrent apply** changes failure semantics: several resources can be tainted after one run. Plan output already marks each one.
- The **100% coverage gate** makes the TUI and plugin subprocess code expensive. Keep the logic in pure functions and models, and leave only one-statement `main.go` files untested.
- **GitHub API rate limit** (60/h unauthenticated) for `dex plugin add` in CI; `GITHUB_TOKEN` covers it.

## Next

1. `/grill-with-docs` on P1–P13, with priority on P5 (proto), P10 (concurrency), P12 (hash), and P13 (commands). Record confirmed terms in `GLOSSARY.md`, and record P4/P5/P6 as ADRs in `docs/adr/` (they're hard to reverse).
2. `/to-tickets`: one GitHub issue per W-step, with blocking edges per the work-order graph.
3. `/implement` per ticket (or `/implement-spec` for the graph); tick ROADMAP boxes as items land.
