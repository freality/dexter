# Research: Go gRPC-web bridge options for the web GUI

**Ticket:** #11 — "Research — Go gRPC-web bridge options for the web GUI" (child of map #10)
**Date:** 2026-10-12
**Branch:** `research/grpc-web-bridge` (throwaway, never merged; this file is a research artifact pointed at from the ticket)
**Convention note:** the repo has no research-notes convention yet; per the research skill's "match the existing convention, and if there is none, put it somewhere sensible and say where", these notes live in `docs/research/`, alongside the existing `docs/adr/`, `docs/plans/`, `docs/agents/` documentation dirs.

---

## TL;DR — recommendation: `connect-go`

Use **connectrpc.com/connect/v2** (connect-go) with the **@connectrpc/connect-es** TS client in `packages/web`.

- Runs **embedded in the Go daemon** — plain `net/http` handlers, no proxy process ([connect-go README](https://github.com/connectrpc/connect-go#readme)).
- Serves **three wire protocols at once** — Connect, gRPC, and gRPC-Web — "including streaming" ([connect-go README](https://github.com/connectrpc/connect-go#readme)); the Connect protocol supports unary, client-, server-, and bidirectional-streaming RPCs, with only bidi requiring HTTP/2 ([Connect protocol spec](https://connectrpc.com/docs/protocol/)).
- dex only needs **server-streaming** (`Apply`/`Destroy` return `stream ApplyResponse` — [proto/adapter.proto](../proto/adapter.proto); the map adds Stats/Logs and live-event fan-out, all server streams — [#10 Notes](https://github.com/freality/dexter/issues/10)) — comfortably covered by all candidates except none; see below.
- **TS codegen from `.proto`** via `protoc-gen-es`/`protoc-gen-connect-es` ([connect-es README](https://github.com/connectrpc/connect-es#readme)); generated code is plain ESM that runs in browsers over the fetch API ([@connectrpc/connect-web](https://www.npmjs.com/package/@connectrpc/connect-web)), so the bundle is built once at dex release time and embedded — end users never need Node (matches map #10 Notes: "static bundle built at dex's own release time … end users still never need Node").
- **License/maintenance:** Apache-2.0; module status **stable** ([connect-go README, "Status: Stable"](https://github.com/connectrpc/connect-go#readme)); v2.0.0 released 2026-10-07, repo pushed 2026-10-09, 15 open issues (GitHub API, 2026-10-12); connect-es v2.2.0 released 2026-09-07.

Runner-up: `improbable-eng/grpc-web`'s `grpcweb` package is the only *pure gRPC-Web wire bridge* that embeds in-process, but the project is in maintenance mode and dormant since 2023, so it is not a safe foundation for the GUI.

---

## Background: what the bridge must carry

From map #10 Notes and the ticket:

- Transport is gRPC daemon-wide, "with streaming for live events"; the browser goes through a gRPC-web/grpc-gateway bridge rather than a parallel REST API ([#10 Notes, "Transport"](https://github.com/freality/dexter/issues/10)).
- The web GUI is a static bundle in a new `packages/web` workspace, built at dex release time, embedded via `go:embed`; end users never need Node ([#10 Notes, "The web GUI"](https://github.com/freality/dexter/issues/10)).
- Streaming RPCs in scope: live apply events and Stats/Logs. Today's [proto/adapter.proto](../proto/adapter.proto) has `rpc Apply(ApplyRequest) returns (stream ApplyResponse)` and `rpc Destroy(DestroyRequest) returns (stream ApplyResponse)` — both **server-streaming** (`ApplyResponse` carries `LOG`, `PROGRESS`, `SUCCESS`, `ERROR` events). **No client-streaming or bidi RPCs exist or are planned** in the ticket scope, so the required bar is: *unary + server-streaming, in-process, with proto→TS codegen*.

---

## Candidate by candidate

### 1. `connect-go` (connectrpc.com/connect/v2) — recommended

**Streaming:** The Connect protocol supports "unary, client streaming, server streaming, and bidirectional streaming RPCs"; bidi requires HTTP/2, all other types work over HTTP/1.1 ([Connect protocol spec](https://connectrpc.com/docs/protocol/)). connect-go handlers "also support the gRPC and gRPC-Web protocols, including streaming, headers, trailers, and error details" ([connect-go README](https://github.com/connectrpc/connect-go#readme)). Server-streaming in the browser works over ordinary fetch-streamed responses — no HTTP/2 requirement.

**Deployment:** Embedded. Handlers are plain `net/http` (`connecthttp.Mount(mux, server)` mounts on any `http.Server`); the v2 core is transport-agnostic and `connectinprocess` even calls a server without a network ([connect-go README](https://github.com/connectrpc/connect-go#readme), v2.0.0 release notes. No Envoy, no proxy.

**Browser codegen:** `@connectrpc/connect-es` (TS) + `protoc-gen-es`/`protoc-gen-connect-es` generate plain TypeScript from `.proto`; `@connectrpc/connect-web` adapters target "web browsers and any other platform that has the fetch API" ([connect-es README](https://github.com/connectrpc/connect-es#readme)). Output is static ESM — builds cleanly into a `packages/web` bundle and requires no Node at end-user runtime. Bonus: because connect-go also *serves the gRPC-Web protocol* natively, generic grpc-web-generated clients would work against it too, without Envoy.

**License/maintenance:** Apache-2.0; "Status: Stable" per the README with semver guarantees within the 2.x series ([connect-go README](https://github.com/connectrpc/connect-go#readme)). v2.0.0 released 2026-10-07 (breaking release was API-only: "The wire protocol, gRPC compatibility, and the rest of the ecosystem are unchanged" — [v2.0.0 release notes](https://github.com/connectrpc/connect-go/releases)); connect-es v2.2.0 released 2026-09-07; both repos pushed within the last week (GitHub API, 2026-10-12). Backed by Buf's ConnectRPC org, the same org behind connect-es, conformance suites, and Buf Studio.

**Trade-off to know:** connect-go is *not* a wrapper around `grpc.Server` — Connect handlers and gRPC-go handlers are generated separately from the same service implementation (protoc-gen-connect-go vs protoc-gen-go-grpc). dex would implement each service once and register it with both a gRPC-go server (CLI/dash/MCP stay on classic gRPC) and a Connect server (browser), or simply serve everything via Connect, since Connect handlers also accept the gRPC wire protocol. Single service implementation, two generated registrations.

### 2. `grpc-gateway` (grpc-ecosystem/grpc-gateway)

**What it is:** not a gRPC-Web bridge at all — it "generates a reverse-proxy server which translates a RESTful JSON API into gRPC" ([grpc-gateway README](https://github.com/grpc-ecosystem/grpc-gateway#readme)). The browser would call JSON/HTTP endpoints, i.e. exactly the "second REST API" the ticket wants the bridge to avoid (albeit one that is code-generated, not hand-written).

**Streaming:** supported features include "Mapping streaming APIs to newline-delimited JSON streams"; non-goals explicitly include "True bi-directional streaming" ([grpc-gateway README, Features](https://github.com/grpc-ecosystem/grpc-gateway#readme)). Server-streaming therefore works (events/logs arrive as ndjson), client/bidi do not — which dex does not need today, but it couples the browser to a JSON/ndjson dialect rather than the protobuf wire.

**Deployment:** embedded via a generated gateway `ServeMux` mounted on the daemon's `net/http` server — in-process, no proxy ([grpc-gateway README](https://github.com/grpc-ecosystem/grpc-gateway#readme)).

**Browser codegen:** no proto→TS client generation of its own; it can emit an OpenAPI (Swagger) v2 spec for third-party codegen ([grpc-gateway README, Features](https://github.com/grpc-ecosystem/grpc-gateway#readme)). Fine at build time, but the typed-client story is weaker than connect-es's first-party `protoc-gen-*` output.

**License/maintenance:** BSD-3-Clause; v2.31.0 released 2026-09-25; repo pushed 2026-10-12; 141 open issues, ~20k stars (GitHub API, 2026-10-12). Healthy and widely used (e.g. Google Cloud Endpoints), but it answers a different question than the ticket: REST gateway, not gRPC-Web.

### 3. `improbable-eng/grpc-web` (grpcweb Go package + @improbable-eng/grpc-web TS client)

**Streaming:** "if you plan on using gRPC-Web you're limited to: unary RPCs … server-side streaming RPCs"; the gRPC-Web spec itself does not support client-side streaming ([improbable-eng README](https://github.com/improbable-eng/grpc-web#readme)); the client has an experimental websocket transport for client/bidi calls ([grpc-web client websocket docs](https://github.com/improbable-eng/grpc-web/tree/master/client/grpc-web/docs/websocket.md)). Server-streaming — what dex needs — is supported.

**Deployment:** the `grpcweb` package "wraps an existing `grpc.Server` as a gRPC-Web `http.Handler`" — runs **embedded** in the Go binary; `grpcwebproxy` is an optional standalone reverse proxy, not required ([improbable-eng README](https://github.com/improbable-eng/grpc-web#readme)). This is the one option that bridges a genuine `grpc.Server` to the gRPC-Web wire in-process, so existing grpc-go registrations carry over unchanged.

**Browser codegen:** `ts-protoc-gen` + `@improbable-eng/grpc-web` runtime give typed TS clients from `.proto`, browser-native ([improbable-eng README](https://github.com/improbable-eng/grpc-web#readme)). Also interoperable with the official grpc-web `protoc-gen-grpc-web` output.

**License/maintenance:** Apache-2.0, but the README itself says: "this repo is in maintenance mode, and we recommend users migrate to the official grpc-web client" ([improbable-eng README](https://github.com/improbable-eng/grpc-web#readme)); last release v0.15.0 (2021-11-04), last push 2023-09-23, 180 open issues (GitHub API, 2026-10-12). Functionally the right shape, operationally a dead end.

### 4. Envoy's gRPC-Web filter

**Streaming:** the official grpc-web client "currently supports 2 RPC modes: Unary … and Server-side Streaming (grpcwebtext mode)"; "Client-side and Bi-directional streaming is not currently supported" ([grpc/grpc-web README](https://github.com/grpc/grpc-web#readme)); the wire spec defers bidi to whatwg streams ([grpc PROTOCOL-WEB.md](https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md)). Server-streaming through Envoy works (the official gRPC-Web hello-world/echo examples proxy a Node gRPC server through Envoy), and the filter's request path buffers the body to end-of-stream, which is consistent with no client/bidi streaming ([grpc_web_filter.cc](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/grpc_web/grpc_web_filter.cc)).

**Deployment:** a **separate proxy process** — "gRPC-web clients connect to gRPC services via a special proxy; by default, gRPC-web uses Envoy" ([grpc/grpc-web README](https://github.com/grpc/grpc-web#readme)). Envoy is not a Go library; the daemon would ship and operate an extra service (config, TLS, lifecycle) for what the other candidates do in-process. Against map #10's single-binary homelab posture ([#10 Notes](https://github.com/freality/dexter/issues/10)).

**Browser codegen:** none — Envoy provides the wire translation only; the browser still needs a grpc-web client (official grpc-web runtime + `protoc-gen-grpc-web`, which supports `import_style=typescript` / `commonjs+dts` ([grpc/grpc-web README](https://github.com/grpc/grpc-web#readme))).

**License/maintenance:** Apache-2.0; Envoy is extremely active (pushed 2026-10-12; 29k stars, but ~1.9k open issues — GitHub API, 2026-10-12). The filter itself is thin and stable. The blocker is operational footprint, not maintenance.

---

## Comparison

| | connect-go | grpc-gateway | improbable grpc-web | Envoy grpc-web filter |
|---|---|---|---|---|
| In-process (no proxy) | ✅ `net/http` handler ([README](https://github.com/connectrpc/connect-go#readme)) | ✅ generated `ServeMux` ([README](https://github.com/grpc-ecosystem/grpc-gateway#readme)) | ✅ `grpcweb` wraps `grpc.Server` ([README](https://github.com/improbable-eng/grpc-web#readme)) | ❌ separate Envoy process ([grpc-web README](https://github.com/grpc/grpc-web#readme)) |
| Unary | ✅ | ✅ | ✅ | ✅ |
| Server-streaming (dex's Apply/Destroy/Stats/Logs) | ✅ Connect/gRPC/gRPC-Web, HTTP/1.1+ ([spec](https://connectrpc.com/docs/protocol/)) | ✅ ndjson only ([README](https://github.com/grpc-ecosystem/grpc-gateway#readme)) | ✅ ([README](https://github.com/improbable-eng/grpc-web#readme)) | ✅ grpcwebtext mode ([README](https://github.com/grpc/grpc-web#readme)) |
| Client/bidi streaming | ✅ (bidi needs HTTP/2) | ❌ explicit non-goal | ❌ spec; ws transport experimental | ❌ spec + filter buffers requests |
| Proto→TS codegen | ✅ connect-es first-party | ⚠️ OpenAPI via 3rd party only | ✅ ts-protoc-gen | ⚠️ needs a grpc-web client |
| License | Apache-2.0 | BSD-3-Clause | Apache-2.0 | Apache-2.0 |
| Last release / activity | v2.0.0 2026-10-07, active | v2.31.0 2026-09-25, active | v0.15.0 2021-11, **maintenance mode** | Envoy active, filter stable |
| Open issues | 15 | 141 | 180 | ~1.9k (repo-wide) |

Maintenance/activity figures from `gh api repos/<owner>/<repo>` (archived flag, license, pushed_at, open_issues, releases/latest) as of 2026-10-12.

---

## Recommendation

**Adopt `connect-go` (`connectrpc.com/connect/v2`) + `@connectrpc/connect-es` for the web GUI.**

1. **Streaming requirement met, in-process:** server-streaming (apply events, Stats/Logs) works over plain HTTP/1.1 fetch streams in the browser ([Connect protocol spec](https://connectrpc.com/docs/protocol/)); the daemon mounts handlers on its own `net/http` server — no Envoy or second process ([connect-go README](https://github.com/connectrpc/connect-go#readme)).
2. **One service implementation, three wires:** with protoc-gen-connect-go, the same handlers also answer classic gRPC (CLI, `dash`, MCP stay on gRPC-go) and the gRPC-Web wire (any generic grpc-web client) — nothing else in the field offers this in-process ([connect-go README](https://github.com/connectrpc/connect-go#readme)).
3. **Build-time TS, runtime-zero-Node:** connect-es emits plain TS/ESM consumed by `packages/web` at release time; the embedded static bundle is served verbatim to browsers ([connect-es README](https://github.com/connectrpc/connect-es#readme); [#10 Notes](https://github.com/freality/dexter/issues/10)).
4. **Healthy and stable:** Apache-2.0, "Stable" status, v2.0.0 days old, few open issues ([connect-go README](https://github.com/connectrpc/connect-go#readme); GitHub API).

**Rejected:** grpc-gateway (creates the REST/JSON API the ticket wants to avoid, JSON-only streaming); improbable-eng/grpc-web (functionally ideal — in-process gRPC-Web over a real `grpc.Server` — but in maintenance mode since 2021, dormant since 2023); Envoy (correct protocol, wrong deployment: a whole proxy process for a single-binary homelab daemon).

**Follow-up note for the daemon ticket:** the one migration cost is codegen shape — connect-go generates its own handlers from the proto, so the daemon's service implementations get registered both as gRPC-go (existing clients) and Connect (browser) from the same business logic. Worth a prototype ticket to confirm the dual registration pattern with the existing `proto/adapter.proto` service.

## Sources (primary)

- connect-go README + "Status: Stable": https://github.com/connectrpc/connect-go#readme
- Connect protocol spec (all four RPC types; HTTP/1.1 vs HTTP/2): https://connectrpc.com/docs/protocol/
- connect-go v2.0.0 release notes: https://github.com/connectrpc/connect-go/releases
- connect-es README (TS web client, browser support window): https://github.com/connectrpc/connect-es#readme
- grpc-gateway README (features: ndjson streaming; non-goals: bidi): https://github.com/grpc-ecosystem/grpc-gateway#readme
- improbable-eng/grpc-web README (maintenance-mode banner; unary+server-streaming only; grpcweb in-process): https://github.com/improbable-eng/grpc-web#readme
- Official grpc-web client README (Envoy by default; unary + server-streaming; TS codegen): https://github.com/grpc/grpc-web#readme
- gRPC-Web wire spec (bidi deferred to whatwg streams): https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md
- Envoy gRPC-Web filter (config overview; request-buffering implementation): https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/grpc_web_filter , https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/grpc_web/grpc_web_filter.cc
- Repo metadata (license, activity, releases, issues): `gh api repos/{grpc-ecosystem/grpc-gateway,connectrpc/connect-go,connectrpc/connect-es,improbable-eng/grpc-web,grpc/grpc-web,envoyproxy/envoy}` as of 2026-10-12
- dex context: `proto/adapter.proto` (server-streaming Apply/Destroy); map #10 Notes (transport, packages/web, no Node at runtime)
