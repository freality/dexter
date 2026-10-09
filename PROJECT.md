# PROJECT.md

## The Vision

`dex` is a unified Control Plane and Infrastructure as Code (IaC) toolchain designed specifically for the homelab and self-hosted ecosystem. Inspired by the ultimate secret home laboratory, `dex` helps you manage massive, infinitely complex infrastructure from the comfort of your own network.

Unlike Terraform, which relies on HCL, `dex` uses standard TypeScript to provide a strictly-typed, programmatic authoring experience. Unlike AWS CDK or Pulumi, `dex` requires **no global Node.js runtime**—it is distributed as a single, statically compiled Go binary that natively transpiles and evaluates TypeScript in a secure, embedded sandbox.

The ultimate goal of `dex` is to evolve from an ephemeral deployment script into a persistent, AI-ready homelab operating system, bridging the gap between declarative provisioning and imperative day-two operations.

## Core Principles

1. **Zero Runtime Friction:** Users drop the binary into their system and start writing TypeScript immediately. No `package.json`, no `ts-node`, no dependency hell required to synthesize the project.
2. **Convention over Configuration:** Inspired by Nuxt, the project relies on file-based auto-discovery (`dex/services/*.ts`) to seamlessly stitch together modular infrastructure.
3. **Pluggability by Design:** The core engine is a dumb orchestrator. Every target environment (Docker, Proxmox, Incus, Tailscale) is managed via isolated, community-driven gRPC plugins.
4. **Separation of State and Logic:** Plaintext secrets are never serialized into the Intermediate Representation (IR) or the state file.
5. **Unified Control:** Infrastructure definitions, management scripts (Custom Commands), and live telemetry live in the same repository and share the same type-safe context.

## High-Level Architecture

The system is divided into three distinct boundaries:

* **The TypeScript SDK (`@dex/iac`):** A set of builder functions (`service`, `volume`, `stack`, `network`) used to author the laboratory. It produces a JSON-based Intermediate Representation (IR).
* **The Core Engine (Go):** The `dex` CLI. It embeds `esbuild` and `goja` to evaluate the SDK, builds the Directed Acyclic Graph (DAG) of dependencies, calculates state drift, and orchestrates concurrency via an internal Event Bus.
* **The Adapters (gRPC Plugins):** Independent binaries (`dex-adapter-docker`, `dex-adapter-proxmox`) that receive standard IR JSON payloads from the core engine and translate them into target-specific API calls.

## Userland Directory Structure

```text
my-laboratory/
├── dex.config.ts              # Core entrypoint (engine setup, plugins, state backend)
├── .env                       # Local environment variables and secrets
└── dex/
    ├── services/              # Auto-discovered infrastructure resources (e.g., solr.ts, web.ts)
    ├── plugins/               # Adapter configurations and credentials
    └── commands/              # Executable TypeScript scripts for day-two ops (e.g., composer-update.ts)

```

## Implementation Roadmap

### Phase 1: The Ephemeral MVP

*Establish the synthesis pipeline and prove the engine.*

* TypeScript SDK stubs (`defineDexConfig`, `service`).
* Embedded `esbuild` + `goja` synthesis inside a Cobra-based Go CLI.
* DAG resolution, local state management (`.dex-state.json`), and drift detection.
* Hardcoded Docker adapter in the Go core to prove the CRUD lifecycle.

### Phase 2: Extensibility & Organization (V1)

*Make it modular, pluggable, and fully equipped.*

* Nuxt-style file auto-discovery for `dex/services/`.
* Decoupled gRPC plugin architecture using `hashicorp/go-plugin`.
* Custom Commands API (`dex run <cmd>`) with injected `Context`.
* Terminal UI (TUI) powered by an internal Event Bus for concurrent deployment rendering.

### Phase 3: The Persistent Control Plane (V2)

*Transition to a background daemon model.*

* Extract execution engine into `dex daemon`. CLI becomes a thin client.
* Web GUI served from the daemon for DAG visualization and real-time statuses.
* `dex dash` interactive TUI for streaming logs and metrics.
* Model Context Protocol (MCP) Server integration for AI assistants.

---

## System Mechanics

### State & Cache Management
To maintain a clean project root, all local engine artifacts are stored in a hidden `.dex/` directory, which must be added to `.gitignore`.
*   **`.dex/state.json`**: The local state backend (when not using a remote backend).
*   **`.dex/plugins/`**: Locally downloaded gRPC adapter binaries.
*   **`.dex/generated/`**: Auto-generated TypeScript `.d.ts` files providing IDE autocomplete for dynamically resolved state variables.

### Plugin Distribution
Plugins are distributed decentrally via Git repository URLs. The CLI fetches pre-compiled binaries natively based on the user's host OS and architecture from the repository's releases.
*   Installation: `dex plugin add github.com/username/dex-adapter-name`

### Licensing
This project is licensed under the MIT License.