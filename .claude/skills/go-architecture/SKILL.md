---
name: go-architecture
description: |
  Architecture review and design guidance for the HSK Vinh University Registration
  System backend. Invoke when evaluating structural changes, adding new domains,
  reviewing package boundaries, or deciding whether an abstraction is justified.
  Operates strictly within the project's existing domain-oriented flat architecture —
  handler → service → SQL. Does not introduce Clean Architecture, DDD, CQRS, or
  other patterns not present in this repository.
---

# go-architecture — Architecture Review and Design

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-architecture <target>
```

`<target>` is a domain, a proposed change, a "should I..." question, or a file path.

---

## The Architecture This Project Uses

Before evaluating any change, internalize this:

```
cmd/api/main.go          ← dependency wiring only
    ↓
internal/<domain>/route.go        ← URL registration + middleware attachment
    ↓
internal/<domain>/handler.go      ← HTTP binding, validation, call service, respond
    ↓
internal/<domain>/service.go      ← business logic + SQL + audit logging
    ↓
database/sql → PostgreSQL 16
```

Supporting layers (no logic):
- `model.go` — DB-mirroring entity structs.
- `mapper.go` — entity → response DTO, pure functions.
- `dto/request/` — input types with validate tags.
- `dto/response/` — output types with json tags.
- `enum/` — typed string constants with `Valid()` + `String()`.

Cross-cutting (shared across domains):
- `internal/middleware/` — HTTP middleware.
- `internal/utils/` — response helpers, validation helper, password util.
- `internal/audit/` — the only exported interface in the project.

---

## Phase 1 — Evaluate the Existing Architecture

When asked about a structural change, first determine:

**Is the existing architecture solving the actual problem?**

- If yes: reject the change. Refactoring for its own sake adds risk without benefit.
- If no: identify the minimum structural change that solves the problem.

Do not introduce Clean Architecture, DDD, CQRS, event sourcing, microservices, hexagonal architecture, or heavy abstraction layers because they are popular. Recommend them only if a concrete problem cannot be solved within the existing structure.

---

## Phase 2 — Package Boundary Rules

### Allowed dependencies

```
handler.go     → service.go, utils/*, middleware/ (constants only)
service.go     → model.go, dto/*, enum/*, audit/, database/sql
mapper.go      → model.go, dto/response/
route.go       → middleware/, gin
cmd/api/main.go → all internal packages (wiring only)
```

### Forbidden dependencies

```
handler.go → database/sql (no SQL in handlers)
service.go → gin, net/http (no HTTP in services)
mapper.go  → service.go (mappers are pure functions)
<domain>/  → <other_domain>/ (except audit and the one payment→registration dependency)
```

### The one cross-domain dependency

`payment.Service` depends on `*registration.Service` — this is an intentional, documented dependency wired in `main.go`. It exists because payment success must trigger registration confirmation. Any new cross-domain dependency must be equally justified and wired through `main.go`.

---

## Phase 3 — Interface Policy

The project has exactly **one exported interface**: `audit.Service`.

It exists for a concrete reason: audit logging is injected into every other service, and a test double (fake) is needed for unit testing.

**Before creating a new interface, verify all of these:**
1. There are (or will be) multiple implementations, OR
2. The concrete type cannot be used in tests (no test double possible).
3. The interface is defined in the package that **uses** it, not the package that implements it.

If only one implementation exists and the type can be used directly in tests, do not create an interface.

---

## Phase 4 — When a New Domain is Justified

A new `internal/<domain>/` directory is justified when:
- The new responsibility has its own tables.
- The new responsibility has its own state machine.
- It would be used independently by multiple callers.

It is **not** justified for:
- Utility functions (add to `internal/utils/`).
- A single new endpoint on an existing domain.
- Grouping by technical concern (e.g., `internal/validators/`).

---

## Phase 5 — When a Shared Package is Justified

`internal/utils/` is the only shared utility package. Its current contents:
- `response.go` — HTTP envelope helpers.
- `validator.go` — `utils.Validate` singleton + `NewValidator()` imperative helper.
- Password hashing helper (if present).

Add to `utils/` only if the function is:
- Genuinely reusable across 3+ domains.
- Has no domain-specific business logic.
- Is a pure or near-pure function.

Do not create `internal/common/`, `internal/helpers/`, `internal/base/`, or similar dumping-ground packages.

---

## Phase 6 — Middleware Policy

All middleware lives in `internal/middleware/`. Middleware is for cross-cutting HTTP concerns only:
- Authentication (`JWTAuth`)
- Authorization (`RequireRole`)
- CORS
- Request logging
- Rate limiting
- Panic recovery

Do not add domain-specific logic to middleware. Do not add middleware in `handler.go` or `service.go`.

---

## Phase 7 — Configuration Policy

All configuration is in `internal/config/config.go`. The `Config` struct is the single source of config values.

Do not read `os.Getenv` outside of `config.go`. Do not add package-level `var` initialized from env vars in domain packages.

---

## Phase 8 — Review Output Format

For each architectural finding or proposal, state:

```
Finding/Proposal: <what>
Current state: <how it is now>
Problem: <what concrete issue this causes — or "none" if it's sound>
Recommendation: <smallest change that solves the problem>
Trade-offs: <what is gained vs. lost>
```

When the existing architecture is sound, explicitly state:
> "The existing structure is appropriate for this requirement. No architectural change needed."

---

## Decision Table

| Question | Answer |
|---|---|
| Should I add a repository layer? | No — services own SQL directly. This is a deliberate choice. |
| Should I add a repository interface? | Only if there will be multiple DB backends. Currently there is one. |
| Should I use Clean Architecture? | No — the current flat structure is sufficient for this domain size. |
| Should I add a new shared package? | Only if used by 3+ domains with no domain-specific logic. |
| Should I add a new interface? | Only if needed for testing or multiple implementations exist. |
| Should I split a service method? | Yes, if it exceeds ~80 lines or has multiple independent responsibilities. |
| Should I add a new domain? | Yes, if it has its own tables and independent state machine. |
| Should I add a goroutine? | Only if you can answer owner, lifetime, stop condition, and error handling. |

---

## Do Not

- Introduce abstractions speculatively ("we might need this later").
- Split packages by technical layer (`handlers/`, `services/`, `repositories/`).
- Create a `common/` package.
- Add the `audit` interface to `notification.Service` or `location.Service` — they do not need it.
- Move SQL to a dedicated query file without a concrete reason.
- Recommend microservices or event sourcing for this scale of application.
