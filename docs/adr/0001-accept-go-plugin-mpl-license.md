# Accept `hashicorp/go-plugin`'s MPL-2.0 license

SPEC.md mandates `hashicorp/go-plugin` for the gRPC adapter architecture, but AGENTS.md requires every dependency to be MIT-compatible; `go-plugin` is MPL-2.0 (verified against GitHub's API, latest v1.8.0), its first non-permissive dependency. We considered hand-rolling a minimal exec+JSON-over-stdio protocol to stay MIT-only, but that means rebuilding process handshake, lifecycle, and streaming from scratch for no real benefit: MPL-2.0 is file-level copyleft, so using it unmodified as an external dependency doesn't obligate relicensing `dex` itself. Decided to accept it (Phase 2 grilling, Q4) and update AGENTS.md's License section to record the sign-off rather than silently violate the stated policy.

Transitive dependencies checked clean: `google.golang.org/grpc` (Apache-2.0), `google.golang.org/protobuf` (BSD-3-Clause), `hashicorp/go-hclog` (MIT).
