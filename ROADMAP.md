# Dex Project Roadmap

This document outlines the phased development plan for `dex`. It serves as a checklist and guide for the implementation of the core CLI, the TypeScript SDK, and the ecosystem of plugins.

## Phase 1: The Ephemeral MVP (Foundation)
**Goal:** Prove the synthesis engine and state reconciliation work. You should be able to write a single `dex.config.ts` file and use the CLI to deploy a Docker container locally.

*   **TypeScript SDK (`@dex/iac`)**
    *   [x] Scaffolding: Create npm package structure.
    *   [x] API Design: Implement `defineDexConfig`, `service()`, and `env()` helpers.
    *   [x] Synthesis: Write the `synth()` function to output the JSON Intermediate Representation (IR).
*   **The Go Core Engine**
    *   [x] CLI Setup: Implement basic Cobra commands (`init`, `plan`, `apply`).
    *   [x] Transpilation: Integrate `esbuild` to transpile TS in memory.
    *   [x] Evaluation: Integrate `goja` to evaluate transpiled JS and capture the JSON IR.
    *   [x] DAG Engine: Implement topological sorting to order resources based on dependencies.
    *   [x] State Management: Implement local read/write to `.dex/state.json` with basic file locking.
    *   [x] Diff Engine: Compare JSON IR against local state to output Adds/Changes/Destroys.
*   **First-Party Adapters**
    *   [x] Hardcoded Docker Adapter: Build a direct Docker API integration in Go to prove the CRUD lifecycle without gRPC overhead.

## Phase 2: Extensibility & Organization (V1)
**Goal:** Make the tool usable for real-world, complex homelabs by supporting modular files, secrets, and third-party plugins.

*   **Modular Architecture**
    *   [ ] Auto-Discovery: Update the Go engine to glob `dex/services/**/*.ts`.
    *   [ ] Virtual Entrypoint: Generate an in-memory entrypoint for `esbuild` to support cross-file imports natively.
*   **The Plugin Ecosystem**
    *   [ ] gRPC Architecture: Implement `hashicorp/go-plugin` in the Go core.
    *   [ ] Extracted Docker Adapter: Move the MVP Docker adapter into a standalone binary (`dex-adapter-docker`).
    *   [ ] Plugin Distribution: Build the `dex plugin add` command to fetch binaries from GitHub Releases into `.dex/plugins/`.
*   **Secrets & Commands**
    *   [ ] Secrets Management: Implement the `secret()` helper, `.env` parsing in Go, SHA-256 hashing for state, and late-binding injection into gRPC requests.
    *   [ ] Custom Commands: Implement `defineCommand` in the SDK and the `dex run <cmd>` execution flow.
*   **Observability**
    *   [ ] Terminal UI (TUI): Integrate a framework (like Bubbletea) to render concurrent deployment progress bars via the internal Event Bus.

## Phase 3: The Persistent Control Plane (V2)
**Goal:** Transition from an ephemeral deployment script to a persistent homelab operating system.

*   **Daemonization**
    *   [ ] Background Server: Extract the execution engine into a `dex daemon` process.
    *   [ ] Thin Client: Refactor the `dex` CLI to send commands to the daemon via gRPC/REST.
*   **Monitoring & Interfaces**
    *   [ ] TUI Dashboard: Build `dex dash` to subscribe to WebSocket streams for live CPU/RAM and logs.
    *   [ ] Web GUI: Serve a React/Vue dashboard directly from the Go daemon for DAG visualization.
*   **AI & Automation**
    *   [ ] MCP Server: Implement the Model Context Protocol in the daemon to expose state and Custom Commands to AI assistants.
    *   [ ] GitOps/Cron: Implement scheduled tasks and repository polling within the daemon.