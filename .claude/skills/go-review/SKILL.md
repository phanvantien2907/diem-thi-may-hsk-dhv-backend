---
name: go-review
description: |
  Senior Go code reviewer for the HSK Vinh University Registration System backend.
  Invoke to review any file, domain, or change set. Produces prioritized, actionable
  findings. Does not modify code unless explicitly asked. Covers correctness, security,
  data consistency, concurrency, error handling, API conventions, and architecture
  boundaries specific to this project.
---

# go-review — Code Review

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-review <target>
```

`<target>` may be a file path, a domain (`internal/registration/`), a Git diff, or a description of the change.

---

## Review Order

Always review in this priority order. Stop escalating severity downward — a critical data-consistency bug is more important than a naming nit.

1. Correctness
2. Security
3. Data consistency
4. Concurrency
5. Error handling
6. API behavior
7. Performance
8. Maintainability / idiomatic Go
9. Architecture boundaries

---

## Checklist by Category

### 1. Correctness

- [ ] Does the code do what the requirement states?
- [ ] Are all business rules from CLAUDE.md §18 respected (state machines for registration, payment, seat)?
- [ ] Does cancellation free the seat? Does payment success confirm the registration?
- [ ] Is `RowsAffected()` checked where an update must affect exactly one row?
- [ ] Are `sql.ErrNoRows` paths handled correctly and not conflated with other errors?

### 2. Security

- [ ] Are all protected routes guarded with `middleware.JWTAuth` in `route.go`?
- [ ] Are admin-only endpoints additionally guarded with `middleware.RequireRole("admin")`?
- [ ] Is the login failure message generic ("invalid credentials") — not distinguishing user-not-found from wrong-password?
- [ ] Are passwords never logged, returned in responses, or stored without bcrypt?
- [ ] Are JWT tokens and the JWT secret never logged?
- [ ] Are error messages returned to clients free of internal DB details (table names, SQL errors)?
- [ ] Is there any string interpolation into SQL? (Must be `$N` placeholders only.)
- [ ] Are soft-deleted records filtered (`AND *_is_deleted = FALSE`) in every query?
- [ ] Are raw entity structs returned as API responses? (They must not be — always map through `mapper.go`.)

### 3. Data Consistency

- [ ] Do operations that must be atomic use a `db.BeginTx` transaction?
- [ ] Is `defer tx.Rollback() //nolint:errcheck` present immediately after every `BeginTx`?
- [ ] Is `tx.Commit()` only called after all writes succeed?
- [ ] Is audit logging performed inside the same transaction as the primary write?
- [ ] Are enum values validated with `.Valid()` before persistence?
- [ ] Do new tables have `*_is_deleted`, `*_created_at`, `*_updated_at`, and a `fn_touch_updated_at` trigger?
- [ ] Do new tables use `BIGINT GENERATED ALWAYS AS IDENTITY` primary keys?
- [ ] Are new foreign keys declared?

### 4. Concurrency

- [ ] Can two concurrent requests corrupt shared state (seats, payments, registrations)?
- [ ] Does seat reservation use `SELECT ... FOR UPDATE SKIP LOCKED` inside a transaction?
- [ ] Does the payment callback use `SELECT ... FOR UPDATE` and check `status = 'pending'` before updating?
- [ ] Does any new goroutine outlive the request context? If so, does it use its own context?
- [ ] Does any new goroutine have a defined stop condition?
- [ ] Is there shared mutable state accessed without synchronization?

### 5. Error Handling

- [ ] Are all errors from DB calls handled — none silently discarded with `_`?
  - Exception: `_ = s.auditSvc.Log(...)` is the established pattern for non-fatal audit.
- [ ] Are errors wrapped with `fmt.Errorf("domain: operation: %w", err)`?
- [ ] Are `errors.Is(err, sql.ErrNoRows)` checks used — not string matching on error messages?
- [ ] Do handlers return the correct HTTP status code for each error class? (See CLAUDE.md §15.)
- [ ] Does `utils.InternalServerError` receive a generic message — not a raw DB error?

### 6. API Behavior

- [ ] Does every handler use `utils.*` helpers — never `c.JSON` directly?
- [ ] Are all parameters parsed with `strconv.ParseInt` via a local `paramID` helper?
- [ ] Are query parameters bound with `c.ShouldBindQuery`?
- [ ] Does the handler call `ShouldBindJSON` and then `utils.Validate.Struct` in that order?
- [ ] Is `utils.Created` (201) used for create operations and `utils.OK` (200) for reads/updates?
- [ ] Is `utils.OKWithMeta` used for paginated list endpoints?

### 7. Performance

- [ ] Are there N+1 queries? (Multiple DB calls inside a loop where one query would suffice.)
- [ ] Are there missing indexes for frequently queried columns (foreign keys, status, soft-delete)?
- [ ] Are large result sets fetched without pagination when a user could have many records?
- [ ] Is `rows.Close()` deferred immediately after `QueryContext`?
- [ ] Is `rows.Err()` checked after the scan loop?

### 8. Maintainability / Idiomatic Go

- [ ] Are functions longer than ~60 lines? Could they be split at a natural boundary?
- [ ] Is there excessive nesting (>3 levels)? Would early returns improve readability?
- [ ] Are named constants used instead of magic strings for statuses and enum values?
- [ ] Are duplicate code blocks present that could use the existing mapper or service?
- [ ] Is there dead code (unreachable returns, unused variables)?
- [ ] Do comments explain intent — not restate what the code obviously does?

### 9. Architecture Boundaries

- [ ] Is there SQL in a handler file? (Must be in service.)
- [ ] Is there `gin.Context` in a service file? (Must not be.)
- [ ] Is mapping logic (field assignments from entity to DTO) outside `mapper.go`?
- [ ] Is a new package named `common/`, `helpers/`, `base/`, or `managers/`?
- [ ] Is a new interface introduced without a concrete need (testing, multiple implementations)?
- [ ] Is business logic duplicated across multiple domains?
- [ ] Is `notification.Service` or `location.Service` receiving `auditSvc`? (They should not.)

---

## Output Format

Report findings grouped by category. For each finding:

```
[SEVERITY] Category — Short description
Location: internal/payment/service.go:42
Finding: <what is wrong>
Impact: <concrete consequence if not fixed>
Suggestion: <specific, minimal fix>
```

Severity levels: `CRITICAL` | `HIGH` | `MEDIUM` | `LOW` | `INFO`

Only report `LOW` and `INFO` if the higher-severity issues are exhausted.

---

## Do Not Report

- Subjective formatting preferences already handled by `gofmt`.
- The `mustAccountID` vs `accountID` naming difference across domains — this is an established project pattern.
- Generic "consider adding tests" without identifying a specific behavioral gap.
- Theoretical issues with no realistic trigger path.

---

## Do Not Modify Code

This skill produces findings only. To apply fixes, use `/go-feature` or ask explicitly.
