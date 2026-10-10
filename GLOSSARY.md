# Dexter

Homelab IaC and control plane. Users declare infrastructure in TypeScript; the `dex` Go engine synthesizes it to a JSON IR, diffs it against saved state, and drives target systems through Adapters.

## Language

**Resource**:
One named piece of infrastructure declared in user code: a type, a Plugin, properties, and dependencies on other Resources.

**Adapter**:
The component that turns a Resource into calls against one target system (Docker, Proxmox, …). It may run in-process inside `dex` or as a separate plugin binary reached over gRPC; the role is the same either way.
_Avoid_: Provider

**Plugin**:
The name a Resource gives in its `plugin` field to select which Adapter manages it (e.g. `docker`).

**Service**:
A long-running workload the user declares with the `service()` builder. Its Resource type is Container.

**Container**:
The Resource type produced by `service()`; what the Docker Adapter materializes on the target host.

**Tainted**:
A Resource whose last create or update failed, so its saved state may not match the target; the next apply replaces it.
_Avoid_: dirty, failed

**Secret**:
A property value declared with `secret()` that is resolved from `.env` at apply time and handed to the Adapter in plaintext, but never written to the IR or `state.json` as plaintext — only a keyed hash travels with it, for diffing.
_Avoid_: secret ref, credential

**Command**:
A day-two operation registered with `defineCommand()` and invoked with `dex run <name>`; runs outside the resource DAG, with host privileges, and reaches resource state only through its `ctx` (hashed properties by default; `ctx.secret()` for a deliberate plaintext read).
_Avoid_: task, script
