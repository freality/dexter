# Research: Go MCP Server Libraries for `dex daemon`

> Ticket: \#14 — Research — Go MCP server libraries for the daemon  
> Date: 2026-10-12  
> Branch: `research/mcp-go-sdk`

## Summary

**Recommendation: use the official `modelcontextprotocol/go-sdk` (v1.8.0, Apache 2.0 / MIT).** It is feature-complete, actively maintained in collaboration with Google, and directly meets every requirement in the wayfinder map for exposing daemon state/plan as MCP resources and opt-in Commands as MCP tools, over a LAN-reachable HTTP transport with custom auth middleware.

---

## 1. Official Go SDK: `modelcontextprotocol/go-sdk`

- **Repository**: https://github.com/modelcontextprotocol/go-sdk
- **Documentation**: https://go.sdk.modelcontextprotocol.io/
- **Latest version**: v1.8.0 (as of 2026-10-12)
- **Stars**: ~5,200 · **Forks**: ~585
- **License**: Apache 2.0 for new contributions, MIT for pre-transition code ([LICENSE](https://github.com/modelcontextprotocol/go-sdk/blob/main/LICENSE))
- **Maintained by**: The MCP organization, in collaboration with Google.

### 1.1 Transport Support

The SDK supports **three transports** (quoting the [protocol lifecycle docs](https://go.sdk.modelcontextprotocol.io/protocol/)):

| Transport | Server-side API | LAN-suitable | Notes |
|-----------|----------------|--------------|-------|
| **stdio** | `StdioTransport{}` | No | Child process stdin/stdout; for local-only or subprocess servers. |
| **Legacy SSE** | `NewSSEHandler()` | Yes | HTTP long-poll SSE (2024-11-05 spec). **Deprecated** — "new deployments should use the streamable transport instead." |
| **Streamable HTTP** | `NewStreamableHTTPHandler()` | **Yes** | The modern transport. Single HTTP endpoint, bidirectional JSON-RPC, supports both request/response and streaming. |

**The streamable HTTP transport is the right choice for dex.** It's a plain `http.Handler` that can be mounted on any Go HTTP mux, bound to a LAN-accessible `:port`, and wrapped with standard HTTP middleware.

Server setup (from the [HTTP example](https://github.com/modelcontextprotocol/go-sdk/tree/main/examples/http)):

```go
server := mcp.NewServer(&mcp.Implementation{Name: "dex", Version: "v0.0.1"}, nil)
// add tools/resources...
handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
    return server
}, nil)
http.Handle("/mcp", handler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

### 1.2 Tool & Resource Registration

The SDK provides two registration APIs:

**Resources** (for exposing dex state/plan as readable data):

```go
// Static resource
s.AddResource(&mcp.Resource{
    URI:     "dex://state/apps",
    MIMEType: "application/json",
}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
    data, _ := json.Marshal(dexState)
    return &mcp.ReadResourceResult{
        Contents: []mcp.ResourceContents{
            &mcp.TextResourceContents{
                URI: "dex://state/apps",
                MIMEType: "application/json",
                Text: string(data),
            },
        },
    }, nil
})

// URI template (e.g., dex://state/apps/{name})
s.AddResourceTemplate(&mcp.ResourceTemplate{
    URITemplate: "dex://state/apps/{name}",
}, handler)
```

**Tools** (for exposing opt-in Commands):

The typed helper is the simplest path — it auto-infers schemas from Go structs:

```go
type ExecInput struct {
    Command string `json:"command" jsonschema:"the command name to execute"`
    Args    string `json:"args,omitempty" jsonschema:"optional arguments"`
}
type ExecOutput struct {
    Result string `json:"result" jsonschema:"the command output"`
}

mcp.AddTool(server, &mcp.Tool{
    Name:        "exec_command",
    Description: "Execute an opt-in dex command",
}, func(ctx context.Context, req *mcp.CallToolRequest, input ExecInput) (*mcp.CallToolResult, ExecOutput, error) {
    output, err := dexEngine.Execute(input.Command, input.Args)
    if err != nil {
        return nil, ExecOutput{}, fmt.Errorf("execution failed: %w", err)
    }
    return nil, ExecOutput{Result: output}, nil
})
```

Capabilities are auto-inferred: adding a resource/tool before `Run()` advertises it in the handshake. Dynamic add/remove after connection sends `notifications/*/list_changed` to clients automatically.

### 1.3 Auth / Custom Headers Middleware

Because `StreamableHTTPHandler` is a plain `http.Handler`, it composes trivially with standard Go HTTP middleware. The SDK ships a built-in `auth.RequireBearerToken` middleware ([auth middleware example](https://github.com/modelcontextprotocol/go-sdk/tree/main/examples/server/auth-middleware)):

```go
handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
    return server
}, nil)

// Wrap with auth
verifier := func(ctx context.Context, token string) (*auth.TokenInfo, error) {
    // verify shared token against dex config
    if token == dexConfig.SharedToken {
        return &auth.TokenInfo{UserID: "dex-user", Scopes: []string{"read"}}, nil
    }
    return nil, auth.ErrInvalidToken
}

authedHandler := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
    Scopes: []string{"read"},
}).Wrap(handler)

http.Handle("/mcp", authedHandler)
```

For stateless operation, `StreamableHTTPOptions.Stateless: true` eliminates session IDs, making every request self-contained — simpler for token-auth scenarios.

For custom-header auth (e.g., `X-Dex-Token`), write a simple middleware:

```go
http.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("X-Dex-Token") != dexConfig.SharedToken {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    handler.ServeHTTP(w, r)
}))
```

### 1.4 MCP-Specific HTTP Headers (2026-07-28+ protocol)

For sessions negotiated on protocol version 2026-07-28+, the SDK recognizes standard MCP headers:
- `Mcp-Protocol-Version` — mirrors the protocol version
- `Mcp-Session-Id` — logical session identifier (omitted in stateless mode)
- `Mcp-Method` — mirrors JSON-RPC `method` field
- `Mcp-Param-{Header}` — per-tool parameter passthrough

These enable HTTP-routing-aware proxies to forward MCP traffic without parsing the JSON-RPC body.

### 1.5 Maintenance Status

- **Very active**: latest release v1.8.0, commits within days.
- **Open issues**: 142 (many are feature requests / discussions).
- **CI**: conformance tests, nightly builds, CodeQL, scorecard.
- **Ecosystem**: acknowledged as the official SDK, maintained by the MCP organization in collaboration with Google.
- The README explicitly acknowledges and thanks `mark3labs/mcp-go` and other third-party SDKs as "viable alternatives" that inspired the official SDK's design.

---

## 2. Alternative: `mark3labs/mcp-go`

- **Repository**: https://github.com/mark3labs/mcp-go
- **Latest version**: v1.1.1 (as of 2026-09-23)
- **Stars**: ~9,100 (larger community, older project)
- **License**: MIT
- **Original author**: Ed Zynda

This is the most popular community MCP Go implementation. It predates and inspired the official SDK.

**Transports**: stdio, SSE, **streamable HTTP** (`server.NewStreamableHTTPServer()`).

**Auth**: No built-in auth middleware — compose with standard HTTP middleware wrapping the handler.

**Registration**: Similar API — `server.AddTool()`, `server.AddResource()`.

**Pros**: Larger star count, earlier to market, battle-tested.
**Cons**: Not the official SDK; the official SDK explicitly adopted the design of `mark3labs/mcp-go` and is now the canonical, spec-first implementation maintained by the MCP org. The official SDK has a richer auth story (built-in `RequireBearerToken`) and more comprehensive documentation.

---

## 3. Recommendation

**Use `github.com/modelcontextprotocol/go-sdk`** for the dex MCP server:

| Requirement | How it's met |
|-------------|-------------|
| LAN-reachable HTTP transport | `NewStreamableHTTPHandler()` — plain `http.Handler`, bind to any `:port` |
| Expose state/plan as MCP resources | `Server.AddResource()` / `AddResourceTemplate()` with JSON-serialized state |
| Expose opt-in Commands as MCP tools | `mcp.AddTool()` with typed Go structs for auto-schema inference |
| Shared-token auth | Compose with `auth.RequireBearerToken` or a custom HTTP middleware |
| License & maintenance | Apache 2.0 / MIT, actively maintained by MCP org + Google |

The SDK is spec-compliant (supports MCP 2026-07-28 through 2024-11-05), has a clean API surface, and the auth middleware example demonstrates exactly the pattern needed for dex's shared-token auth plan.

### 3.1 Migration/Integration Notes

- Add `github.com/modelcontextprotocol/go-sdk` as a dependency (Go 1.23+ required).
- The MCP server can run on a dedicated goroutine inside the dex daemon process, or as a sidecar — the streamable HTTP transport makes either trivial.
- Auth granularity: tools can inspect `req.Extra.TokenInfo` at runtime to enforce tool-level permission checks (e.g., read-only users vs. command executors).
- Dynamic resource updates: resources and tools can be added/removed at runtime; the SDK auto-notifies connected clients.