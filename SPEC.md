# Dex Technical Specification
This document defines the technical architecture, data contracts, and implementation rules for `dex`, a Homelab Infrastructure as Code (IaC) CLI. 

Use this specification to guide code generation, system design, and component boundaries.

## 1. Technology Stack
*   **Core CLI & Engine:** Go (1.21+)
*   **SDK & Userland Code:** TypeScript (7.x; native-compiler performance for `tsc` type-checking during SDK build and user `dex.config.ts` authoring)
*   **Embedded Transpiler:** `github.com/evanw/esbuild/pkg/api`
*   **Embedded JS Runtime:** `github.com/dop251/goja`
*   **Plugin System:** `github.com/hashicorp/go-plugin`
*   **CLI Framework:** `github.com/spf13/cobra`
*   **License:** MIT

## 2. Source Code Architecture (Monorepo)
The repository contains both the Go CLI and the TypeScript SDK.

```text
dex-monorepo/
├── cmd/
│   └── dex/               # Main Go binary entrypoint (Cobra setup)
├── pkg/
│   ├── engine/            # State, DAG, Diff, and core orchestration logic
│   ├── jsenv/             # esbuild and goja wrappers for TS execution
│   └── plugin/            # gRPC protobuf definitions and go-plugin scaffolding
├── adapters/              # First-party Go plugins
│   └── docker/            # Default adapter; ships as its own dex-adapter-docker gRPC binary
│       └── cmd/dex-adapter-docker/  # Binary entrypoint (plugin.Serve)
├── packages/
│   └── sdk/               # @dex/iac TypeScript npm package
│       ├── src/
│       ├── package.json
│       └── tsconfig.json
└── proto/                 # Protobuf definitions for plugin communication
```

## 3. Userland Project Structure
When a user initializes `dex`, the CLI expects and manages the following structure:

```text
user-project/
├── dex.config.ts          # CLI config, state backend, and plugin setup
├── .env                   # KEY=VALUE, # comments, optional `export ` prefix, one level of quotes stripped.
│                         # Process env overrides .env. Values reach IR only through env(); secret() values
│                         # never do — they resolve to a token (never plaintext) until apply-time injection.
├── dex/
│   ├── services/          # Auto-discovered TS files (the infrastructure)
│   └── commands/          # Auto-discovered day-two operations (defineCommand, `dex run <name>`)
└── .dex/                  # CLI cache (Added to .gitignore)
    ├── state.json         # JSON state file
    ├── state.json.lock    # File lock to prevent concurrent runs
    ├── plugins/           # Downloaded gRPC adapter binaries (`dex plugin add`/`dex plugin install`)
    └── secret.key         # Random 32-byte key (generated on first use) for HMAC-hashing secret tokens
```

## 4. System Boundaries & Execution Flow

### Step 1: Synthesis (TS -> JSON IR)
1. The Go CLI discovers `dex.config.ts` (its absence is the "not a dex project" error), then every
   `dex/services/**/*.ts` and `dex/commands/**/*.ts`, in sorted slash-path order. `*.d.ts` files and
   dot-prefixed directories are skipped; a missing `dex/services/` or `dex/commands/` is fine.
2. `pkg/jsenv` passes the files to `esbuild` to bundle into a single JS string in-memory.
3. The string is executed inside `goja`.
4. The TS SDK (`@dex/iac`) intercepts the execution and outputs a JSON Intermediate Representation (IR).

### Step 2: Orchestration (Go Engine)
1. The Go Engine parses the JSON IR.
2. It builds a Directed Acyclic Graph (DAG) using `depends_on` and `reference` tokens.
3. It loads `.dex/state.json`.
4. It compares the IR against the State to generate a diff (Create, Update, Delete).

### Step 3: Execution (gRPC Plugins)
1. The Engine schedules the plan's changes as a dependency graph and applies independent resources
   concurrently (a fixed worker limit), waiting on `depends_on` edges, a replace's create on its own
   delete, and deletes after every create/update.
2. It resolves each adapter lazily (`dex plan` never starts a plugin process) and sends JSON payloads
   over gRPC to the resulting `dex-adapter-<name>` binary.
3. Adapters execute the API calls (e.g., Docker API) and stream logs/progress back via gRPC; the CLI
   renders them as a live per-resource TUI in a terminal, or plain log lines otherwise.
4. The Engine saves state after every attempt, success or failure alike (a failed change is stored
   tainted), under a lock that serializes concurrent writers.

## 5. Core Data Contracts

### 5.1 The JSON Intermediate Representation (IR)
The JS sandbox MUST output this exact shape to the Go Engine. Plaintext secrets are NEVER included here; only token pointers.

```json
{
  "project": "my-homelab",
  "plugins": ["github.com/freality/dexter@v1.0.0"],
  "resources": {
    "main": {
      "type": "network",
      "plugin": "docker",
      "properties": {},
      "depends_on": []
    },
    "postgres": {
      "type": "container",
      "plugin": "docker",
      "properties": {
        "image": "postgres:15",
        "env": {
          "POSTGRES_PASSWORD": {
            "$dex_type": "secret",
            "provider": "env",
            "key": "DB_PASSWORD"
          }
        }
      },
      "depends_on": ["main"]
    }
  }
}
```

- Resource keys are the user-supplied names (`/^[a-z0-9][a-z0-9._-]*$/`, unique across all types); `depends_on` entries are those keys.
- `plugins` (D9) is always present (`[]` when empty), deduplicated and order-preserved (not sorted — specs have
  no dependency relationship). Each entry is `github.com/<owner>/<repo>[@tag]`; `dex plugin install` installs
  everything listed, and `dex plugin add <repo>[@tag]` installs one spec outside the declared set.
- `plugin` (singular, on each resource) must match `/^[a-z0-9][a-z0-9-]*$/`; the engine resolves it to a
  `dex-adapter-<name>` binary lazily, at apply time.
- `depends_on` is always present (`[]` when empty), deduplicated and sorted. The SDK rejects unknown targets and self-dependencies; multi-node cycles are the engine's job.
- Serialization is key-sorted at every level (byte-stable for state hashing).
- Host contract: before evaluating the bundle, the engine sets `globalThis.__dex_env` to a plain object of string env vars; after evaluation it calls `globalThis.__dex_synth()`, which returns the IR as a JSON string. SDK failures throw `DexError` with a stable `code` field.

### 5.2 The gRPC Adapter Interface (`proto/adapter.proto`)
All plugins must implement this service. Operations should be idempotent. `Configure`/`Read` from the
original design are dropped for now — nothing consumes them yet; add them back when a consumer lands.

```protobuf
syntax = "proto3";
package dex.plugin;

service Adapter {
  rpc Apply(ApplyRequest) returns (stream ApplyResponse);
  rpc Destroy(DestroyRequest) returns (stream ApplyResponse);
}

message ApplyRequest {
  string resource_id = 1;
  string type = 2;
  string properties_json = 3;        // The raw properties block from the IR
  string prior_properties_json = 4;  // Empty on create
}

message DestroyRequest {
  string resource_id = 1;
  string type = 2;
  string properties_json = 3;
  string outputs_json = 4;
}

message ApplyResponse {
  enum Type { LOG = 0; PROGRESS = 1; SUCCESS = 2; ERROR = 3; }
  Type type = 1;
  string message = 2;                // PROGRESS puts an integer percent here
  string new_state_json = 3; // The materialized state to save (e.g., assigned IPs)
}
```

A stream MUST end with exactly one terminal `SUCCESS` or `ERROR`; a stream that ends without one is an
error (`plugin "<name>": stream ended without result`). Secret values are resolved to plaintext only in
`properties_json`/`prior_properties_json` at the moment of the call — adapter outputs MUST NOT echo a
secret value back (the engine rejects the change if one does).

## 6. Development Directives for AI Agents
*   **Go Code:** Strictly avoid CGO. Use standard library `context` for all cancellations. Keep the CLI stateless; all state is passed in or read from `.dex/state.json`.
*   **TS Code:** Ensure the `@dex/iac` SDK has zero Node.js dependencies (`fs`, `path`, etc.) so it can run purely in the `goja` ECMAScript environment. 
*   **Error Handling:** All Go errors must wrap context (`fmt.Errorf("failed to parse: %w", err)`).
*   **Secrets:** When handling a `{"$dex_type": "secret"}` token, the Go engine must resolve it from the host `.env` AT RUNTIME and inject it directly into the gRPC `ApplyRequest`. Do not write it to the state file. Hash it (HMAC-SHA256 keyed by per-project `.dex/secret.key`, per ADR-0002) for state comparisons.