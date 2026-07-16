# Accepted Architecture

**Last reviewed:** `<YYYY-MM-DD>`

This document describes architecture that has been accepted. Proposals and speculative designs belong in review artifacts until approved. Add `decisions/` when a consequential choice needs a durable ADR.

## System context

`<Describe the system, its users, and external systems in one short section or diagram.>`

## Boundaries and ownership

| Boundary | Owns | Does not own |
| --- | --- | --- |
| `<component or domain>` | `<state and responsibilities>` | `<explicit exclusions>` |

## Dependency direction

```text
<entrypoints> -> <application/domain> -> <ports/contracts> -> <adapters>
```

- `<State dependency rules that must remain true.>`

## Data ownership and persistence

| Data | Source of truth | Writer | Readers | Retention |
| --- | --- | --- | --- | --- |
| `<data class>` | `<store or system>` | `<owner>` | `<consumers>` | `<policy>` |

## Security invariants

- `<Credential, authorization, isolation, or data-handling rule>`
- `<Evaluator or CI gate that enforces the invariant>`

## Accepted components

| Component | Responsibility | Contract |
| --- | --- | --- |
| `<component>` | `<single responsibility>` | `<API, interface, schema, or protocol>` |

## Architecture checks

- `<Executable command or eval that detects boundary drift>`
