---
name: go-feature
description: |
  Primary implementation workflow for the HSK Vinh University Registration System backend.
  Invoke when implementing any new feature, endpoint, or domain logic. Follows the
  project's domain-oriented architecture (handler → service → SQL), audit logging
  requirements, state machine rules, and concurrency patterns as defined in CLAUDE.md.
  Use this skill for all greenfield feature work.
---

# go-feature — Feature Implementation Workflow

**Source of truth for all conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-feature <feature requirement>
```

---

## Phase 1 — Understand

1. Read the requirement carefully. Identify:
   - The owning domain(s) from `internal/`: `auth`, `candidate`, `exam`, `registration`, `payment`, `result`, `certificate`, `notification`, `location`, `audit`, `media`.
   - Whether this touches an existing domain or requires a new one.
   - All HTTP endpoints affected (method, path, auth level).
   - All database tables affected.

2. Before writing a single line of code, inspect:
   ```bash
   # Read the owning domain's full layout
   cat internal/<domain>/service.go
   cat internal/<domain>/handler.go
   cat internal/<domain>/route.go
   cat internal/<domain>/model.go
   cat internal/<domain>/mapper.go
   ```

3. For any new migration, inspect:
   ```bash
   ls migrations/
   cat migrations/001_schema.sql   # understand existing table patterns
   ```

4. Identify reuse opportunities: existing service methods, mappers, enums, validation patterns.

---

## Phase 2 — Analyse

Before proposing anything, answer these questions explicitly:

**Business rules**
- What invariants must hold? (e.g., one registration per candidate per session)
- What state transitions are involved? Refer to CLAUDE.md §18 for existing state machines.
- What constitutes a business rule violation vs. an infrastructure error?

**Validation**
- What fields require `validate` struct tags on the request DTO?
- What enum values must be validated with `.Valid()` in the service?
- What database constraints back up the validation?

**Database**
- Does this need a new table? → New migration file `migrations/00N_description.sql`.
- Does this need new indexes? → Based on actual query patterns, not speculation.
- Does this need `RETURNING`? → Always scan immediately.
- Soft-delete: always filter `AND *_is_deleted = FALSE`.

**Transactions**
- Which operations must be atomic? Keep the transaction as small as practical.
- Does audit logging belong inside this transaction? (Yes, for all state-changing operations.)
- Use `defer tx.Rollback() //nolint:errcheck` immediately after `BeginTx`.

**Concurrency/Idempotency**
- Can two requests race on the same row? → Use `SELECT ... FOR UPDATE` or `SELECT ... FOR UPDATE SKIP LOCKED`.
- Can a callback be delivered twice? → Check existing status before transitioning.
- Does capacity need to be guarded? → Database locking, not application-level checks.

**If requirements are materially ambiguous — stop and ask.** Do not implement based on assumptions about business rules, authorization levels, or state transitions.

---

## Phase 3 — Propose

Write a concise implementation plan covering:
- Files to create (new domain, new migration, new DTOs, new enums).
- Files to modify (existing service, handler, route, mapper, model).
- Wire-up changes in `cmd/api/main.go`.
- Any migration.

Wait for approval before proceeding.

---

## Phase 4 — Implement

Follow the exact file layout from CLAUDE.md §2 and these rules:

### New domain checklist
```
internal/<domain>/
├── dto/request/<domain>_request.go   # validate tags on all fields
├── dto/response/<domain>_response.go # json tags
├── enum/<domain>_enum.go             # Valid() + String() on every type
├── entities/<table>_entities.go      # *Entity structs + optional input structs (CreateXInput, etc.)
├── mapper.go                         # pure To<X>ResponseDTO functions
├── service.go                        # NewService(db, auditSvc) *Service
├── handler.go                        # NewHandler(svc) *Handler
└── route.go                          # RegisterRoutes(r, h, jwtSecret)
```

Wire in `cmd/api/main.go` last.

### Existing domain — add one endpoint
1. DTO → service method → mapper → handler method → route registration.
2. Audit log inside the transaction for every state-changing operation.

### Mandatory patterns

**Handler — thin:**
```go
func (h *Handler) DoThing(c *gin.Context) {
    var req request.DoThingRequestDTO
    if err := c.ShouldBindJSON(&req); err != nil {
        utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
        return
    }
    if err := utils.Validate.Struct(req); err != nil {
        utils.BadRequest(c, err.Error())
        return
    }
    result, err := h.svc.DoThing(c.Request.Context(), req, utils.MustAccountID(c))
    if err != nil {
        utils.Conflict(c, err.Error()) // or NotFound, InternalServerError
        return
    }
    utils.Created(c, result) // or OK
}
```

**Service — transaction with audit:**
```go
func (s *Service) DoThing(ctx context.Context, req request.DoThingRequestDTO) (response.ThingResponseDTO, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return response.ThingResponseDTO{}, fmt.Errorf("domain: begin tx: %w", err)
    }
    defer tx.Rollback() //nolint:errcheck

    // ... queries using $N placeholders ...

    _ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
        ActorAccountID: &actorID,
        Action:         auditenum.AuditActionCreate.String(),
        EntityTable:    auditenum.AuditEntitySomething.String(),
        EntityID:       e.SomeID,
        NewValue:       newVal,
    })

    if err := tx.Commit(); err != nil {
        return response.ThingResponseDTO{}, fmt.Errorf("domain: commit: %w", err)
    }
    return ToThingResponseDTO(e), nil
}
```

**Enum — always Valid() + String():**
```go
type ThingStatus string
const (
    ThingStatusActive ThingStatus = "active"
)
func (s ThingStatus) Valid() bool { return s == ThingStatusActive }
func (s ThingStatus) String() string { return string(s) }
```

**Route protection:**
```go
// JWT only
g.Use(middleware.JWTAuth(jwtSecret))
// JWT + admin
g.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
```

### Do not
- Write business logic in handlers.
- Call `c.JSON` directly — use `utils.*`.
- Use `context.Background()` inside request processing (exception: Cloudinary cleanup on rollback in `candidate.Service`).
- Define per-domain local `mustAccountID` or `paramID` helpers — use `utils.MustAccountID(c)` and `utils.ParamID(c, "key")`.
- Add repositories, interfaces, or DI frameworks.
- Touch unrelated files.

---

## Phase 5 — Verify

```bash
gofmt -l ./...
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

Fix any compilation errors or vet warnings before proceeding.

---

## Phase 6 — Self-Review

Before declaring done, answer:
- Does every handler use `utils.*` helpers only?
- Does every state-changing service method audit-log inside the transaction?
- Does every query filter `AND *_is_deleted = FALSE`?
- Does every enum field call `.Valid()` before persistence?
- Does every transaction have `defer tx.Rollback()`?
- Are soft-delete columns present on every new table?
- Are `$N` placeholders used — no string interpolation?
- Are context values propagated through every DB call?

Fix any `no` answer, then re-run tests.

---

## Phase 7 — Summary

Provide a concise summary:
- Files created/modified.
- New endpoints with method, path, auth level.
- New DB tables or columns.
- Any known limitations or follow-up work needed.
