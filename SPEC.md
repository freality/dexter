# Dex Technical Specification

This document defines the technical architecture, data contracts, and implementation rules for `dex`, a Homelab Infrastructure as Code (IaC) CLI. 

Use this specification to guide code generation, system design, and component boundaries.

## 1. Technology Stack
*   **Core CLI & Engine:** Go (1.21+)
*   **SDK & Userland Code:** TypeScript (5.x)
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
│   └── docker/            # Default MVP adapter (dex-adapter-docker)
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
├── .env                   # Local secrets (never serialized to IR)
├── dex/
│   └── services/          # Auto-discovered TS files (the infrastructure)
└── .dex/                  # CLI cache (Added to .gitignore)
    ├── state.json         # JSON state file
    ├── state.json.lock    # File lock to prevent concurrent runs
    └── plugins/           # Downloaded gRPC binaries
```

## 4. System Boundaries & Execution Flow

### Step 1: Synthesis (TS -> JSON IR)
1. The Go CLI globs `dex.config.ts` and `dex/services/**/*.ts`.
2. `pkg/jsenv` passes the files to `esbuild` to bundle into a single JS string in-memory.
3. The string is executed inside `goja`.
4. The TS SDK (`@dex/iac`) intercepts the execution and outputs a JSON Intermediate Representation (IR).

### Step 2: Orchestration (Go Engine)
1. The Go Engine parses the JSON IR.
2. It builds a Directed Acyclic Graph (DAG) using `depends_on` and `reference` tokens.
3. It loads `.dex/state.json`.
4. It compares the IR against the State to generate a diff (Create, Update, Delete).

### Step 3: Execution (gRPC Plugins)
1. The Engine iterates through the DAG concurrently.
2. It sends JSON payloads over gRPC to the appropriate adapter binaries.
3. Adapters execute the API calls (e.g., Docker API) and stream logs/status back via gRPC.
4. The Engine writes the updated state back to `.dex/state.json`.

## 5. Core Data Contracts

### 5.1 The JSON Intermediate Representation (IR)
The JS sandbox MUST output this exact shape to the Go Engine. Plaintext secrets are NEVER included here; only token pointers.

```json
{
  "project": "my-homelab",
  "resources": {
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
      "depends_on": ["network.main"]
    }
  }
}
```

### 5.2 The gRPC Adapter Interface (`proto/adapter.proto`)
All plugins must implement this service. Operations should be idempotent.

```protobuf
syntax = "proto3";
package dex.plugin;

service Adapter {
  // Verifies credentials and setup
  rpc Configure(ConfigureRequest) returns (ConfigureResponse);
  
  // Reads live state from the target
  rpc Read(ReadRequest) returns (ReadResponse);
  
  // Server-streaming to allow real-time progress/logs back to the CLI TUI
  rpc Apply(ApplyRequest) returns (stream ApplyResponse);
  
  rpc Destroy(DestroyRequest) returns (stream DestroyResponse);
}

message ApplyRequest {
  string resource_id = 1;
  string properties_json = 2; // The raw properties block from the IR
}

message ApplyResponse {
  enum Type { LOG = 0; PROGRESS = 1; SUCCESS = 2; ERROR = 3; }
  Type type = 1;
  string message = 2;
  string new_state_json = 3; // The materialized state to save (e.g., assigned IPs)
}
```

## 6. Development Directives for AI Agents
*   **Go Code:** Strictly avoid CGO. Use standard library `context` for all cancellations. Keep the CLI stateless; all state is passed in or read from `.dex/state.json`.
*   **TS Code:** Ensure the `@dex/iac` SDK has zero Node.js dependencies (`fs`, `path`, etc.) so it can run purely in the `goja` ECMAScript environment. 
*   **Error Handling:** All Go errors must wrap context (`fmt.Errorf("failed to parse: %w", err)`).
*   **Secrets:** When handling a `{"$dex_type": "secret"}` token, the Go engine must resolve it from the host `.env` AT RUNTIME and inject it directly into the gRPC `ApplyRequest`. Do not write it to the state file. Hash it (SHA-256) for state comparisons.