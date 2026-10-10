# Phase 1 completion plan: Hardcoded Docker Adapter

The last unchecked Phase 1 item in `ROADMAP.md`. Everything else (SDK, CLI, esbuild/goja, DAG, state, diff) is landed.

**Goal (ROADMAP Phase 1):** `dex init && dex apply` deploys the scaffolded `whoami` container to local Docker; editing `dex.config.ts` updates it; removing the service destroys it.

## Current state

- `engine.Provider` (`pkg/engine/provider.go`) already defines the seam: `Create`/`Update`/`Delete` returning outputs that `Engine.applyChange` persists to `state.Resource.Outputs`.
- `newProductionEngine` (`cmd/dex/internal/cli/root.go`) registers nothing, so `dex apply` fails today with `no provider registered for plugin "docker"`.
- `diff.Compute` diffs IR against state only; `type`/`plugin` changes already become delete+create.
- `task test:go` enforces 100% statement coverage on `./pkg/...` and `./cmd/dex/...`.

## Decisions

Confirmed across three grilling rounds (Q1–Q16). Each cites its grounding where one was needed.

| # | Decision |
|---|----------|
| D1 | Scope: type `container` only. `volume`/`network` return `docker: unsupported type %q`. |
| D2 | Location: **top-level** `adapters/docker` module (module path `github.com/freality/dexter/adapters/docker`, matching the `cmd/dex` → `github.com/freality/dexter/cmd/dex` convention), per SPEC §2's tree diagram — not nested under `pkg/`. Added to `go.work` as a third `use` entry. `cmd/dex` reaches it through the workspace, no `replace` directive needed. Imports nothing from `pkg/engine`; a thin wrapper in `cmd/dex/internal/cli` adapts it to `engine.Adapter`. Still open for Phase 2: AGENTS.md places standalone adapter binaries at `cmd/dex-adapter-<target>`, SPEC's tree has no separate binary path — decide where `main` lives when extracting the gRPC binary. |
| D3 | Client: `github.com/moby/moby/client` v0.6.x (Apache-2.0). `github.com/docker/docker` is deprecated as of Docker v29 (moby/moby discussion #52404); `moby/moby/client` is its replacement. |
| D4 | Endpoint: `client.FromEnv` + API version negotiation — `DOCKER_HOST` or the default local socket. No Docker contexts, no ssh hosts until Phase 2 `Configure`. |
| D5 | Update: parse both `cur.Properties` and desired `Properties` through `parseSpec`, compare field-by-field. If only `Restart` differs, call `ContainerUpdate` in place (one Docker API call, no recreation — confirmed against `moby/moby`'s `client/container_update.go`: `ContainerUpdateOptions{RestartPolicy: *container.RestartPolicy}`), then one `ContainerInspect` for the `{"id"}` output (D13), since the engine overwrites `Outputs` with whatever `Update` returns. If `ContainerUpdate` reports not-found (container removed by hand, invisible per D9), fall back to `Create` — same not-found tolerance as the full-replace path and D9's `Delete`; without it every later apply would retry the fast path and fail forever. Otherwise full replace: pull the new image first (so a failed pull leaves the old container running), then stop, remove, create, start. Plan still renders `~ update` either way. |
| D6 | Container name = resource name. Label `dev.dex.resource=<name>`. Project label/prefix deferred — `Adapter.Create` doesn't carry the project and nothing needs it yet (one project per Docker host is the assumed Phase 1 layout). |
| D7 | Properties: `image` (string, required), `ports` (`"host:ctr[/proto]"` strings, optional — omitted means no published port, e.g. an internal-only service), `env` (string→string), `command` (string array, exec form only — no shell-form string), `restart` (Docker restart policy name: `no`, `always`, `on-failure`, or `unless-stopped`, the default; no retry count). Any other key, or wrong value type, is an error. |
| D8 | Pull only when the image is absent locally. `:latest` drift across applies is invisible; accepted for Phase 1. |
| D9 | No `Read`/refresh — belongs with the gRPC `Read` RPC (SPEC §5.2), Phase 2. `Delete` treats "no such container" as success, and never removes a container without the dex label (see D17). |
| D10 | Rename `engine.Provider` → `engine.Adapter` (interface, file, error text, fakes); terms recorded in `GLOSSARY.md`. |
| D11 | `Create` finds an existing container with that name (detected by the create's name-conflict error, not a pre-check): if it carries `dev.dex.resource=<name>`, treat it as an orphan from a dead apply, remove and recreate. Otherwise fail with `container %q exists and is not managed by dex` rather than touch something dex didn't create. |
| D12/D16 | After `ContainerStart`, a fixed 500ms delay then one `ContainerInspect`. If `State.Running == false` or `State.Restarting` (a crash loop under the restart policy), fail with the exit code in the error message; the delay ends early if the context is cancelled and **leave the container in place** — reuses D11's orphan-recreate on the next apply instead of a second cleanup path, and keeps `docker logs <name>` available for debugging. `ponytail:` fixed delay is a known-ceiling heuristic (misses slow crashes); upgrade path is a short poll loop or a real health check, only if it proves too short in practice. |
| D13 | Outputs: `{"id": "<container id>"}`. Update/Delete address containers by name, so a missing or stale id never blocks them; recorded for future visibility (`dex show`/`dex state`). |
| D14 | Stop with Docker's default timeout, then remove. No `Force` kill, no configurable grace period. |
| D15 | Integration test lives at `adapters/docker/docker_integration_test.go` behind `//go:build docker`; the coverage gate runs untagged, so it never needs a daemon. |
| D17 | Added during implementation, after the smoke run found that a failed apply saved nothing: after a crashed replace, reverting the config planned "No changes" while nothing ran. Engine: a failed adapter `Create`/`Update` records the resource **tainted** (desired properties and dependencies), and `Apply` saves state after every change attempt. Diff: a tainted resource still in config is replaced (delete+create pair); one removed from config is deleted. Plan output marks it `(tainted: last apply failed)`. Because a tainted replace or delete now runs `Delete` on whatever holds the name, the adapter only stops/removes containers carrying the dex label. |

## Steps

1. **Rename the seam (D10).** Use LSP rename `engine.Provider` → `engine.Adapter`. Rename `pkg/engine/provider.go` → `adapter.go` (and `_test.go`), the `providers` field → `adapters`, and the error text to `no adapter registered for plugin %q`. Rename the fakes in `pkg/engine/engine_test.go`, `pkg/engine/provider_test.go` and `cmd/dex/internal/cli/fakes_test.go`, and update the package comment in `engine.go`.
   Check: `go build ./pkg/... ./cmd/dex/... && go test ./pkg/engine/... ./cmd/dex/...`.

2. **Scaffold the module (D2).** `mkdir -p adapters/docker && cd adapters/docker && go mod init github.com/freality/dexter/adapters/docker`. Add `./adapters/docker` to `go.work`. `go get github.com/moby/moby/client@latest && go mod tidy` (D3). Confirm the license of every newly added module is MIT-compatible. Add `adapters/**/*.go` to `Taskfile.yml`'s `build:go` sources, and `./adapters/docker/...` to the `test:go` command (`./adapters/...` does not match from the repo root: the workspace module root is `adapters/docker`).
   Check: `go build ./adapters/...`; `task --list` still resolves; `go work sync`.

3. **Property decoding (D7)** in `adapters/docker/spec.go`:
   - `type spec struct{ Image string; Ports []string; Env map[string]string; Command []string; Restart string }`
   - `func parseSpec(props map[string]any) (spec, error)` rejects unknown keys and wrong types with `fmt.Errorf("property %q: ...")` errors; `Image` required, `Ports` may be empty, `Restart` defaults to `unless-stopped`.
   - Port strings map onto the client's port binding types.
   - `func (s spec) sameExceptRestart(other spec) bool` for the D5 fast-path check.

   Check: no direct tests of `parseSpec`/`sameExceptRestart` (unexported internals). Their cases — missing image, unknown key, non-string env value, malformed port, restart default, restart-only vs other change — are tested through the `Adapter` seam in Step 4.

4. **Lifecycle** in `adapters/docker/docker.go`:
   - `type API interface` (exported) holds only the client methods used: image inspect/pull, container create/start/stop/remove/inspect/update. It is the Docker system boundary, so tests fake it here and in `cmd/dex` (Step 5).
   - `type Adapter struct{ api API }` with `func New() (*Adapter, error)` (FromEnv + negotiation) and `func NewWithAPI(api API) *Adapter` for injecting a fake.
   - `Create(ctx, name, typ string, props map[string]any) (map[string]any, error)`: type check (D1), parse, pull if missing (D8, drain the pull stream), orphan handling (D11), create with label (D6), start, **crash check** (D12/D16: sleep 500ms, inspect once, fail with exit code if not running, leave container), return `{"id"}` (D13).
   - `Update(ctx, name string, desired, cur map[string]any) (map[string]any, error)`: parse both, `sameExceptRestart` → `ContainerUpdate` with the new `RestartPolicy` only, then `ContainerInspect` for `{"id"}`; not-found from `ContainerUpdate` → delegate to `Create`; else stop+remove then delegate to `Create` (D5).
   - `Delete(ctx, name string) error` = `removeOwned`: inspect; a free name or foreign container is left alone, else stop+remove (D9, D14, D17). The same helper does the D11 claim and the full-replace removal.

   Every error wrapped with context (SPEC §6).
   Check (seam 1: `Adapter` methods over a fake `API`): every branch, including D7 property errors, D5 restart-only fast path and its not-found fallback, D11 orphan vs foreign container, D12/D16 crash-check failure, D9 delete not-found. `go test -cover ./adapters/docker` must show 100%.

5. **Wire it in.** In `cmd/dex/internal/cli`, a small `dockerAdapter` struct adapts `ir.Resource`/`state.Resource` to the plain-type calls, reached through the workspace (`go.work` resolves `adapters/docker` for the `cmd/dex` module without a `replace` directive). `newProductionEngine` builds it via `docker.New()` and does `Registry.Register("docker", …)`, returning the error it already allows for.
   Check (seam 2: CLI `apply`): no forwarding-only wrapper test. A `cmd/dex/internal/cli` test runs `apply --yes` through a real engine with `docker.NewWithAPI(fake)` registered, and asserts on output and saved state (`Outputs.id`) for create, update and delete. This covers the wrapper for the gate. The `docker.New()` error path is covered by its own test. `task test:go` passes the gate.

6. **Integration test (D15, seam 3).** `adapters/docker/docker_integration_test.go` (`//go:build docker`) runs Create → restart-only Update (confirm container id unchanged) → full-replace Update (port change, confirm id changes) → Delete against the real daemon with `traefik/whoami`, using a unique resource name, and cleans up in `t.Cleanup`. Covers what the fakes can't: real Docker error shapes (not-found) and real id behaviour.
   Check: `go test -tags docker ./adapters/docker/...`.

7. **End-to-end smoke** in a temporary directory with `bin/dex` (built by `task build`):

   | Step | Expected |
   |---|---|
   | `dex init` | `created dex.config.ts` … |
   | `dex apply --yes` | `+ whoami`, `whoami: done`, `Apply complete: 1 added, 0 changed, 0 destroyed.` |
   | `curl -s localhost:8080` | whoami response containing `Hostname:` |
   | `docker inspect whoami --format '{{index .Config.Labels "dev.dex.resource"}}'` | `whoami` |
   | `dex plan` | `No changes. Infrastructure matches configuration.` |
   | edit `restart` only; `dex apply --yes`; `docker inspect whoami --format '{{.Id}}'` before/after | `~ whoami (restart)`; container id **unchanged** (D5 fast path) |
   | `docker rm -f whoami`, then edit `restart` only; `dex apply --yes` | `~ whoami (restart)` succeeds; `whoami` running again (D5 not-found fallback to `Create`) |
   | edit `ports` to `8081:80`; `dex apply --yes`; compare id before/after | `~ whoami (ports)`; container id **changes** (full replace); `curl localhost:8081` answers |
   | set `command: ["--no-such-flag"]` (arguments to whoami's entrypoint, so it exits 2); `dex apply --yes` | apply fails with `exit code 2`; `docker ps -a --filter name=whoami` still shows the dead container (D12/D16); state marks it tainted |
   | revert `command`, apply again | plans `- whoami (tainted: last apply failed)` + `+ whoami`; container running (D17) |
   | add a crashing `command` to a new service, apply, then remove the service and apply | the tainted dead container is deleted (D17) |
   | `docker run -d --name whoami` a foreign container, apply, then remove the service and apply | create fails as not managed by dex; the delete leaves the foreign container running (D17) |
   | remove the `service(...)` call; `dex apply --yes` | `- whoami`, `Apply complete: 0 added, 0 changed, 1 destroyed.`; `docker ps -a --filter name=whoami` is empty |
   | `docker rm` a deployed container by hand, then remove it from config and apply | the delete succeeds (D9) |

8. **Docs.** Tick the ROADMAP checkbox. Document `DOCKER_HOST`, the supported container properties, and the new `adapters/` workspace module in README.

## Risks

- `moby/moby/client` is pre-1.0, so its API may churn — including `ContainerUpdate`, used for the D5 fast path. Pin the version in `go.mod`; the `docker.API` interface confines the blast radius to that interface, its implementation, and the two test fakes (`adapters/docker`, `cmd/dex/internal/cli`).
- On macOS, `/var/run/docker.sock` exists only if Docker Desktop's default socket option is on. Otherwise `DOCKER_HOST=unix://$HOME/.docker/run/docker.sock` is required. The README must say so.
- If the host port is already taken, start fails after create, leaving a created-but-stopped container. D11 cleans it up on the next apply. The error must surface Docker's message.
- With D5's full-replace path, an update means downtime: a window between remove and start. A failure in that window taints the resource (D17), so the next apply replaces it whatever the config says.
- With D12/D16, the fixed 500ms crash-check delay is a known-ceiling heuristic: a container that crashes after a longer init reports `whoami: done` and is **not** tainted, so state claims it is fine. Upgrade path noted in D12/D16; not built until needed.
- A failure before the adapter touches anything (e.g. a pull error) still taints, so the next apply replaces a container that was untouched. Accepted: one needless restart.
- With D7, unknown properties are rejected only during `apply`, after confirmation. Plan-time validation needs a validate hook (Phase 2 `Configure`/`Read`).
- `:latest` drift is invisible (D8).
