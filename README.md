# 🧪 Dexter

**The programmatic, declarative Infrastructure-as-Code (IaC) control plane built for homelabs.**

Dexter allows you to define, deploy, and manage your entire homelab infrastructure using standard, strictly-typed TypeScript.

Unlike Terraform, there's no custom HCL to learn. Unlike Pulumi or AWS CDK, **Dexter requires absolutely no Node.js runtime or `package.json` to operate**. It is distributed as a single, statically compiled Go binary (`dex`) that embeds its own V8 JavaScript engine to synthesize your code on the fly.

## ✨ Features

* **Zero Runtime Friction:** Drop the `dex` binary into your path and start coding. No `node_modules`, no `ts-node`, no dependency hell.

* **Convention Over Configuration:** Inspired by frameworks like Nuxt, Dexter automatically globs and wires together infrastructure defined in your `dex/services/` directory.

* **True Programmatic IaC:** Use `if/else` statements, loops, and custom factory functions to dynamically generate stacks across development and production environments.

* **Pluggable Architecture:** The Go core orchestrates the execution, but independent gRPC adapter plugins (Docker, Proxmox, Tailscale) do the heavy lifting.

* **Built-in State Management:** State is tracked locally in a `.dex/` cache directory to detect drift, with native file-locking to prevent concurrent run conflicts.

* **Secure Secret Handling:** Plaintext secrets are never serialized into the state file or Intermediate Representation.

## 🚀 Getting Started

### 1. Install Dexter

Download the latest pre-compiled `dex` binary for your OS from the [Releases](#) page and place it in your `$PATH`.

### 2. Initialize your Laboratory

Create a new directory for your infrastructure and run the init command:

```bash
mkdir my-laboratory && cd my-laboratory
dex init
```

This will scaffold your `dex.config.ts`, your local `.env` file, and the `dex/services/` directory.

### 3. Define a Service

Create a new file in `dex/services/postgres.ts`:

```typescript
import { service, env, secret } from "@dexter/iac";

export default service("postgres", {
  type: "container",
  plugin: "docker", // Built into dex for now; a standalone plugin later
  properties: {
    image: `postgres:${env("POSTGRES_VERSION", "15")}`,
    env: {
      POSTGRES_PASSWORD: secret("DB_PASSWORD") // Fetched securely from .env at runtime
    }
  }
});
```

### 4. Plan and Apply

Preview the changes Dexter will make to your infrastructure, then apply them:

```bash
dex plan
dex apply
```

### Docker adapter

Resources with `plugin: "docker"` and type `container` (what `service()` emits) run on the Docker daemon named by `DOCKER_HOST`, or the default local socket when it is unset. On macOS, if Docker Desktop's default socket is turned off, set `DOCKER_HOST=unix://$HOME/.docker/run/docker.sock`. Docker contexts are not read.

| Property  | Type             | Notes |
|-----------|------------------|-------|
| `image`   | string, required | Pulled only if not already on the daemon, so a moved `:latest` tag is not picked up. |
| `ports`   | string[]         | `"host:container[/proto]"`, e.g. `"8080:80"`. Omit for an unpublished service. |
| `env`     | object of strings | |
| `command` | string[]         | Arguments to the image's entrypoint (Docker `Cmd`), exec form. |
| `restart` | string           | `no`, `always`, `on-failure`, or `unless-stopped` (default). |

Any other property is an error. The container is named after the resource and labelled `dev.dex.resource=<name>`; dex never removes or replaces a container without that label. Changing only `restart` updates the container in place; any other change replaces it.

If a create or update fails (including a container that exits right after starting), the resource is marked **tainted** in `.dex/state.json` and the next `dex apply` replaces it. The failed container is left in place until then, for `docker logs`.

The adapter lives in its own Go workspace module, `adapters/docker`. `task check` covers it with fakes; `go test -tags docker ./adapters/docker/... ./cmd/dex/...` runs the adapter and the full `dex` CLI against a real daemon.

### 5. Run a Command

Day-two operations are TypeScript files anywhere `dex` discovers config (typically `dex/commands/`), registering with `defineCommand`:

```typescript
import { defineCommand } from "@dex/iac";

defineCommand("backup", {
  description: "Snapshot the database volume",
  run: (ctx) => ctx.exec(["docker", "run", "--rm", "-v", "pg_data:/data", "postgres:15", "pg_dump", "-f", "/data/backup.sql"]),
});
```

`dex run` with no arguments lists the registered commands; `dex run backup [args...]` runs one with no eval timeout (a command is interactive day-two ops, not bounded synthesis), cancelling on Ctrl-C and killing any exec'd child. Inside `run`, `ctx.args` carries the trailing CLI arguments, `ctx.env(key)` and `ctx.secret(key)` read `.env`/environment values (plaintext only via the deliberate `secret()` call — `ctx.resources` mirrors `.dex/state.json` with secret properties still hashed), and `ctx.exec(argv)` runs a host process with stdio attached and returns its exit code, which becomes `dex run`'s exit code.

## 🔐 Secrets

Secrets are declared in resource properties with `secret("KEY")`; the engine resolved the value at apply time from your project's `.env` file (or the host environment, following the same lookup order as `env()`). The SDK emits a `{"$dex_type": "secret", "provider": "env", "key": "KEY"}` token, so **plaintext never enters the IR or `state.json`** — only that token and a keyed hash of the value.

```typescript
properties: {
  env: {
    POSTGRES_PASSWORD: secret("DB_PASSWORD") // resolves from .env at apply time
  }
}
```

The hash is HMAC-SHA256 keyed by a random per-project key at `.dex/secret.key`, generated on first use and gitignored along with the rest of `.dex/` — see [ADR-0002](docs/adr/0002-hmac-secret-hashing.md) for why it is keyed rather than plain SHA-256. The hash exists only so `dex plan` can detect drift; it is never reversible to the plaintext.

Losing `.dex/secret.key` is harmless: the next `dex plan` treats every secret as changed, `dex apply` re-sends the same `.env` values through the normal update path, and a fresh key is written.

## 📂 Project Structure

A typical Dexter project looks like this:

```text
my-laboratory/
├── dex.config.ts          # Core engine setup and plugin configuration
├── .env                   # Local environment variables and secrets
└── dex/
    ├── services/          # Auto-discovered infrastructure resources
    ├── plugins/           # Adapter configurations 
    └── commands/          # Executable day-two operations (`defineCommand` + `dex run`)
```

## 📖 Documentation

* [Architecture Specification (SPEC.md)](SPEC.md) - Deep dive into the internal Go/TS mechanics, DAG execution, and gRPC contracts.

* [Project Roadmap (ROADMAP.md)](ROADMAP.md) - See what is planned for Phase 1 (MVP), Phase 2 (Extensibility), and Phase 3 (The Daemon Control Plane).

## 🤝 Contributing

Contributions are welcome! Please read the `SPEC.md` to understand the system boundaries before submitting a PR.

## 📄 License

Dexter is open-source software licensed under the **MIT License**.