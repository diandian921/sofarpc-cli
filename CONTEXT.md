# SofaRPC Agent Interface

This context defines the current product language for an agent-facing SofaRPC invocation system.

## Language

**MCP-first**:
The product stance where MCP tools are the primary interface for agents, while human-facing commands are secondary entry points.
_Avoid_: CLI-first, service-first

**Stdio MCP Server**:
An MCP server process launched and owned by an MCP host, communicating over standard input and output.
_Avoid_: always-on local service

**Pure-Go Direct Runtime**:
The built-in Go runtime that invokes SofaRPC over direct BOLT/Hessian2 without a sidecar process.
_Avoid_: sidecar runtime, background service

**Schema-guided Plain JSON**:
The argument input style where callers provide ordinary JSON values and the system uses local Java source schema knowledge to infer parameter order and Java type names.
_Avoid_: mandatory typed argument DSL, untyped raw JSON

**Exact Invoke**:
An invocation where the caller supplies `paramTypes` and `orderedArguments` explicitly.
_Avoid_: saved case, replay fixture

**InvocationPlan**:
The resolved application-layer representation of one call: project/server or
explicit endpoint, service, method signature, Java-aware arguments, timeout,
warnings, and diagnostics.
_Avoid_: direct tool payload as execution contract

**Java TypedValue**:
The internal value tree that carries Java type names and DTO field type metadata
from planning into Hessian encoding.
_Avoid_: magic keys in user JSON such as `@type`, `__type`, or `__fieldTypes`

## Relationships

- **MCP-first** makes MCP tools the primary interface for agents.
- **Stdio MCP Server** is the process shape; the MCP host owns lifetime.
- **Pure-Go Direct Runtime** is the only runtime path for invoke and ping.
- **InvocationPlan** is the single execution contract for MCP and CLI invoke.
- **Java TypedValue** is the only DTO/type metadata path into the Hessian writer.
- **Schema-guided Plain JSON** is used when local Java source can resolve a method signature.
- **Exact Invoke** remains available when schema resolution is missing or ambiguous.

## Flagged Ambiguities

- "MCP server" means **Stdio MCP Server**, not an always-on local service.
- "去掉 Java 进程" is now resolved as **Pure-Go Direct Runtime only**.
- "复杂参数" is resolved as **Schema-guided Plain JSON** by default, with **Exact Invoke** as the fallback.
- "schema 来源" is local Java source only; no Git download, source jar download, Maven dependency loading, or class loading.
- "invoke 栈" is a single application stack: MCP and CLI both route through `internal/app`; the old `internal/invoker` stack is removed.
