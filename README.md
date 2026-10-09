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
  plugin: "docker", // Requires the dex-adapter-docker plugin
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

## 📂 Project Structure

A typical Dexter project looks like this:

```text
my-laboratory/
├── dex.config.ts          # Core engine setup and plugin configuration
├── .env                   # Local environment variables and secrets
└── dex/
    ├── services/          # Auto-discovered infrastructure resources
    ├── plugins/           # Adapter configurations 
    └── commands/          # Executable day-two operations (Coming Soon)
```

## 📖 Documentation

* [Architecture Specification (SPEC.md)](SPEC.md) - Deep dive into the internal Go/TS mechanics, DAG execution, and gRPC contracts.

* [Project Roadmap (ROADMAP.md)](ROADMAP.md) - See what is planned for Phase 1 (MVP), Phase 2 (Extensibility), and Phase 3 (The Daemon Control Plane).

## 🤝 Contributing

Contributions are welcome! Please read the `SPEC.md` to understand the system boundaries before submitting a PR.

## 📄 License

Dexter is open-source software licensed under the **MIT License**.