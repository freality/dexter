# Dexter

Homelab IaC and control plane. Users declare infrastructure in TypeScript; the `dex` Go engine synthesizes it to a JSON IR, diffs it against saved state, and drives target systems through Adapters.

## Language

**Resource**:
One named piece of infrastructure declared in user code: a type, a Plugin, properties, and dependencies on other Resources.

**Adapter**:
The component that turns a Resource into calls against one target system (Docker, Proxmox, …). It may run in-process inside `dex` or as a separate plugin binary reached over gRPC; the role is the same either way.
_Avoid_: Provider (legacy name for the in-process `engine.Provider` interface, being renamed to `Adapter`)

**Plugin**:
The name a Resource gives in its `plugin` field to select which Adapter manages it (e.g. `docker`).

**Service**:
A long-running workload the user declares with the `service()` builder. Its Resource type is Container.

**Container**:
The Resource type produced by `service()`; what the Docker Adapter materializes on the target host.
