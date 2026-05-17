# SofaRPC Agent Interface

This context defines the product language for an agent-facing SofaRPC invocation system.

## Language

**MCP-first**:
The product stance where MCP tools are the primary interface for agents, while human-facing commands are secondary entry points.
_Avoid_: CLI-first, daemon-first

**Human-facing command**:
A command-line entry point intended for direct use by a person.
_Avoid_: Primary interface, main product

**Stdio MCP Server**:
An MCP server process launched and owned by an MCP host, communicating over standard input and output.
_Avoid_: Background MCP daemon, always-on local service

**Java Engine**:
The SofaRPC execution engine that uses the Java SofaRPC ecosystem to perform calls behind the MCP interface.
_Avoid_: Primary product, user-facing daemon

**Schema-guided Plain JSON**:
The argument input style where callers provide ordinary JSON values and the system uses DTO schema knowledge to infer Java types.
_Avoid_: Mandatory typed argument DSL, untyped raw JSON

**Explicit DTO Description**:
A caller-initiated request to describe a Java DTO before using that schema to plan invocation arguments.
_Avoid_: Fully automatic method discovery

**Case**:
A saved SofaRPC invocation that can be replayed or adapted later.
_Avoid_: Log entry, raw request history

**Project Case**:
A case intended to be shared by people and agents working in the same project.
_Avoid_: Personal scratch case

**Project Case Directory**:
The `sofarpc-cases/` directory in a project, used for shared cases.
_Avoid_: `cases/`, `.sofarpc/cases/`

**User Case**:
A case intended for one user's local reuse and not assumed to be shareable.
_Avoid_: Team fixture, project asset

## Relationships

- **MCP-first** makes MCP tools the primary interface for agents.
- A **Human-facing command** may exist under **MCP-first**, but it is not the primary interface.
- A **Stdio MCP Server** is the default process shape for the **MCP-first** product.
- A **Java Engine** may sit behind a **Stdio MCP Server**, but it is not the primary product interface.
- **Schema-guided Plain JSON** is the default argument input style for MCP invocation.
- **Explicit DTO Description** supplies schema knowledge for **Schema-guided Plain JSON**.
- A **Case** can be either a **Project Case** or a **User Case**.
- A **Project Case** is preferred for shared reuse; a **User Case** is preferred for private or sensitive calls.
- A **Project Case** belongs in the **Project Case Directory**.

## Example dialogue

> **Dev:** "Should this capability be designed first as a CLI subcommand or an MCP tool?"
> **Domain expert:** "Because the project is **MCP-first**, design the MCP tool contract first, then expose a **Human-facing command** if people need it."

> **Dev:** "Should the MCP entry point run as a background service?"
> **Domain expert:** "No — use a **Stdio MCP Server** so the MCP host owns the process lifetime."

> **Dev:** "Does keeping Java in V1 make the product daemon-first?"
> **Domain expert:** "No — the product remains **MCP-first**; the **Java Engine** is only the execution engine behind the MCP tools."

> **Dev:** "Should agents write explicit Java type wrappers for every argument?"
> **Domain expert:** "No — use **Schema-guided Plain JSON** by default, and only require explicit type hints when schema knowledge is insufficient."

> **Dev:** "Can invocation always infer argument DTOs from service and method names?"
> **Domain expert:** "Not in V1 — use **Explicit DTO Description** and cached schema knowledge instead of promising fully automatic method discovery."

> **Dev:** "Should this successful invocation be saved for the team?"
> **Domain expert:** "Save it as a **Project Case** only if it is safe and generally useful; otherwise keep it as a **User Case**."

> **Dev:** "Where should shared invocation cases live in the repository?"
> **Domain expert:** "Use the **Project Case Directory**, `sofarpc-cases/`, so they are visible and distinct from local scratch data."

## Flagged ambiguities

- "改成 MCP" was resolved as **MCP-first**, not as a CLI-first system with an MCP adapter.
- "MCP server" was resolved as **Stdio MCP Server**, not as an always-on local daemon.
- "去掉 Java 进程" was deferred for V1: the resolved term is **Java Engine**, an implementation detail behind the MCP interface.
- "复杂参数" was resolved as **Schema-guided Plain JSON** for the default input style, not mandatory typed wrappers.
- "schema 来源" was resolved as **Explicit DTO Description** plus cache reuse, not fully automatic method discovery in V1.
- "case 存储" was resolved as a two-layer model: **Project Case** for shared assets and **User Case** for local reuse.
- "项目共享 case 目录" was resolved as the **Project Case Directory**, `sofarpc-cases/`.
