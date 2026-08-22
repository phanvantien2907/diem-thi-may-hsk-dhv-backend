---
name: go-refactor
description: |
  Safe, production-oriented refactoring workflow for the HSK Vinh University
  Registration System backend. Invoke when asked to clean up, simplify, or
  improve existing Go code without changing observable behavior. Operates at
  function, file, domain, or repository scope. Enforces behavior preservation
  above all else — correctness, data consistency, and concurrency safety are
  never traded for aesthetics. Uses only patterns present in this repository.
---

# go-refactor — Safe Refactoring Workflow

**Source of truth for all conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-refactor <target>
```

`<target>` may be:
- A specific function: `internal/payment/service.go ProcessCallback`
- A file: `internal/audit/service.go`
- A domain: `internal/registration/`
- A description: `"clean up the candidate service"`, `"remove unnecessary complexity from auth"`
- Repository-wide: `"repository-wide cleanup"`

---

## Core Principle

```
CORRECTNESS > DATA CONSISTENCY > CONCURRENCY SAFETY > BEHAVIOR PRESERVATION
> SECURITY > MAINTAINABILITY > PERFORMANCE > STYLE
```

**Refactoring is not rewriting.**

A refactor must preserve externally observable behavior unless:
- the user has explicitly requested a behavior change, OR
- the current behavior is demonstrably incorrect AND the user has approved the fix.

Never refactor merely because another implementation looks cleaner.
Every change must have a concrete, articulable reason.

---

## Step 1 — DISCOVER: Read Before Touching

Before writing a single line, inspect the target and its context.

### For a target function or file

```bash
cat internal/<domain>/service.go      # implementation
cat internal/<domain>/handler.go      # HTTP callers
cat internal/<domain>/route.go        # middleware, auth
cat internal/<domain>/model.go        # entity structs
cat internal/<domain>/mapper.go       # mapping functions
cat internal/<domain>/enum/           # typed enums
cat internal/<domain>/dto/request/    # input types
cat internal/<domain>/dto/response/   # output types
```

Also find all callers of the target function across the codebase:
```bash
grep -rn "FunctionName" internal/
grep -rn "FunctionName" cmd/
```

### For transaction-related code — mandatory additional inspection

Before touching any method that calls `BeginTx`, `tx.Rollback`, `tx.Commit`, or `auditSvc.Log`:

1. Identify the transaction owner (which function calls `BeginTx`).
2. Identify all DB operations inside the transaction.
3. Identify whether audit logging uses `tx` or `nil` — this distinction is **intentional**:
   - `tx != nil` = audit must be atomic with the business write (state changes: create, update, cancel, confirm).
   - `tx = nil` = audit is standalone, eventual (login event in `auth.Service.Login`).
4. Identify whether `RowsAffected()` is checked — this guards against not-found after concurrent deletes.
5. Never remove a transaction to make code shorter.
4. Never make the audit non-transactional as a side effect of refactoring.

### For concurrency-sensitive code — mandatory additional inspection

For any function touching `exam_seats`, `exam_registrations`, or `payments`:

1. Identify whether `SELECT ... FOR UPDATE` or `SELECT ... FOR UPDATE SKIP LOCKED` is present.
2. Understand why — removing either changes correctness under concurrent load.
3. Check whether the `pending_payment → confirmed` and `pending → success` state transitions are guarded.
4. Never replace these with application-level checks.

### For `payment.ProcessCallback` specifically

Note the two-transaction design:
1. Transaction 1: lock payment row via `FOR UPDATE`, validate `status = 'pending'`, update payment, audit, commit.
2. After commit: call `regSvc.ConfirmRegistration` — a separate transaction.

This is not a bug. It is intentional — payment is committed before registration confirmation so that a registration failure does not roll back a successful payment. Do not merge these into one transaction without explicit approval and understanding of the failure modes.

---

## Step 2 — DIAGNOSE: Classify the Problem

Before proposing changes, classify each finding explicitly. Not all findings are equal.

| Category | Examples in this codebase |
|---|---|
| Readability | Very long SQL strings that could use `const q = ...` alignment |
| Duplication | Repeated query column lists in SELECT vs. RETURNING |
| Excessive complexity | Dynamic WHERE builder in `audit.List` — evaluate carefully before changing |
| Poor function boundaries | Service method doing 4 unrelated things |
| Dead code | Unreferenced helpers, commented-out blocks |
| Error handling problems | Error swallowed with blank `_`, wrong error type returned |
| Transaction problems | Transaction opened but not needed; or needed but absent |
| Concurrency problems | Race on shared state without DB lock |
| N+1 queries | Loop calling DB inside |
| Unnecessary abstraction | Interface with one implementation and no test requirement |

Distinguish:
- **Cosmetic** — style only, safe to defer
- **Maintainability** — reduces future bugs, low risk
- **Correctness issue** — current behavior is wrong, higher risk to change
- **Performance** — evidence required

---

## Step 3 — SAFETY CHECK: Define What Must Not Change

Before proposing any change, explicitly list the behaviors that must remain unchanged.

### Always preserve

- HTTP methods, routes, and status codes.
- Request field names, types, and validation rules.
- Response field names and structure.
- Authentication and authorization rules.
- State machine transitions (registration, payment, seat — see CLAUDE.md §18).
- Transaction atomicity: what is currently in a transaction must remain in a transaction.
- Audit semantics: `tx` vs `nil` distinction, action type, entity type.
- Soft-delete filters (`AND *_is_deleted = FALSE`).
- Enum validation (`.Valid()` before persistence).
- Error messages surfaced to the client (changing these can break clients).
- `RowsAffected()` checks that guard not-found.

### High-risk areas — be especially conservative

- `registration.ReserveSeat`: duplicate check + `FOR UPDATE SKIP LOCKED` + seat hold + INSERT — all inside one transaction. The sequence matters.
- `registration.ConfirmRegistration` and `CancelRegistration`: `FOR UPDATE` lock before status check — must remain.
- `payment.ProcessCallback`: `FOR UPDATE` idempotency guard — must remain. Two-transaction design — must remain unless user explicitly approves merging.
- `audit.Log` tx/nil dispatch — intentional design, not a mistake.
- `auth.Login`: `_, _` on `UPDATE account_last_login_at` — intentional (best-effort, non-critical).

---

## Step 4 — PLAN: Propose Before Implementing

For anything beyond a trivially safe change (renaming a local variable, extracting a SQL constant), write a plan first.

The plan must state:
1. What will change and why.
2. What behavior is guaranteed to remain unchanged.
3. What the risk level is.
4. Whether tests are needed before refactoring.

**Stop and ask if:**
- The scope is ambiguous (does "clean up the service" mean the whole file?).
- A change could affect a state machine transition.
- A change touches a transaction boundary.
- A change touches concurrency-sensitive code.
- A change modifies an API response structure.

---

## Step 5 — IMPLEMENT: Smallest Safe Change

Apply the minimal correct refactor. Follow the actual patterns in this codebase:

### Idiomatic patterns to follow (established in this repo)

**Error wrapping:**
```go
// Correct — follow this format:
return fmt.Errorf("domain: operation: %w", err)

// Not:
return err   // loses context
return fmt.Errorf("error: %v", err)  // loses error identity
```

**Not-found mapping:**
```go
// Correct — preserve error identity:
if errors.Is(err, sql.ErrNoRows) {
    return ..., errors.New("entity not found")
} else if err != nil {
    return ..., fmt.Errorf("domain: query: %w", err)
}
```

**Non-fatal audit:**
```go
// Correct — _ is intentional, non-fatal:
_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{...})
// Do not change to:
if err := s.auditSvc.Log(...); err != nil { return ..., err }
// unless the business requirement explicitly demands audit failure to abort the operation.
```

**Transaction pattern:**
```go
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
    return ..., fmt.Errorf("domain: begin tx: %w", err)
}
defer tx.Rollback() //nolint:errcheck
// ... queries ...
if err := tx.Commit(); err != nil {
    return ..., fmt.Errorf("domain: commit: %w", err)
}
```

**Response helpers — no `c.JSON` directly:**
```go
utils.OK(c, data)
utils.Created(c, data)
utils.BadRequest(c, "invalid request body")
utils.NotFound(c, err.Error())
utils.InternalServerError(c, "failed to fetch records")  // generic message
```

**SQL constants for readability:**
```go
// Acceptable to extract to const when SQL string is reused or very long:
const sqlGetAccount = `
    SELECT account_id, ...
    FROM accounts
    WHERE account_id = $1 AND account_is_deleted = FALSE`
```

### What not to do

- Do not split `service.go` into multiple files unless the file is genuinely unnavigable (400+ lines).
- Do not create a `helpers.go` or `util.go` inside a domain package for one-off functions.
- Do not create new interfaces. The project has exactly one (`audit.Service`) — for a concrete reason.
- Do not extract DB queries into a `queries.go` unless there is a repetition problem.
- Do not rename the `accountID` / `mustAccountID` helpers inside handler files — they are a deliberate per-domain pattern.
- Do not add `context.Background()` anywhere in request-handling code.
- Do not convert `errors.New("...")` to a sentinel error variable unless there is a caller using `errors.Is`.

---

## Step 6 — VERIFY: Run Checks

After every change:

```bash
gofmt -l ./...          # format check (no output = clean)
go vet ./...            # static analysis
go build ./...          # compilation
go test ./...           # all tests (currently none)
go test -race ./...     # race detector (important for registration/payment)
```

If tests exist for the modified code, run them first before refactoring (characterization), then run again after.

---

## Step 7 — DIFF REVIEW: Inspect What Changed

After implementing, review the diff:

```bash
git diff
```

For each changed line ask:
- Does this change observable behavior?
- Does this change what's inside or outside a transaction?
- Does this change a `tx` vs `nil` audit call?
- Does this remove a lock (`FOR UPDATE`, `FOR UPDATE SKIP LOCKED`)?
- Does this change an error message that reaches the client?
- Does this change a status code?
- Is this change related to the requested refactoring scope?

If the answer to any of these is unexpected, revert and re-examine.

---

## Step 8 — REPORT: Summarize What Was Done

### Format

```
## Refactored
- <file>: <specific change>

## Why
- <concrete problem> — <why this improves the code>

## Behavior Preserved
- Transaction semantics: [unchanged / describe]
- Concurrency behavior: [unchanged / describe]
- API behavior (status codes, response shape): [unchanged]
- State machine transitions: [unchanged]
- Audit semantics (tx/nil dispatch): [unchanged]

## Verified
- gofmt: clean
- go vet: clean
- go build: clean
- go test: [result]
- go test -race: [result]

## Remaining Concerns
- [Only real concerns — do not invent problems]
```

---

## Scope Levels

### Level 1 — Target refactor

`/go-refactor internal/audit/service.go`

Modify only the specified target. Inspect its callers to understand impact. Do not touch other files unless a caller must be updated.

### Level 2 — Domain refactor

`/go-refactor internal/registration/`

Inspect all files in the domain. Refactor where justified. Explicitly state what was examined but left unchanged.

### Level 3 — Repository-wide cleanup

`/go-refactor repository-wide cleanup`

**Do NOT immediately modify everything.**

1. Inspect all domains.
2. Produce a prioritized findings report:

```
CRITICAL   — correctness or data-safety issues
HIGH       — error handling problems, transaction issues, concurrency gaps
MEDIUM     — maintainability improvements with meaningful impact
LOW        — cosmetic, style, minor readability
```

3. Wait for explicit approval before applying changes.
4. Apply one category at a time, not all at once.

---

## Anti-Patterns to Refuse

Never do the following even if asked in general terms to "clean up" or "refactor":

| Refused action | Reason |
|---|---|
| Remove `defer tx.Rollback()` | Correctness — prevents connection leak on error |
| Make audit `tx` calls standalone (`nil`) for state-changing ops | Breaks audit atomicity |
| Merge `payment.ProcessCallback`'s two transactions | Changes failure semantics |
| Remove `SELECT ... FOR UPDATE` or `FOR UPDATE SKIP LOCKED` | Breaks concurrent correctness |
| Replace `errors.Is(err, sql.ErrNoRows)` with string matching | Fragile |
| Extract repository interface for single implementation | Unnecessary abstraction |
| Add `context.Background()` in request handlers | Breaks request cancellation |
| Remove `RowsAffected()` checks | Silently ignores not-found |
| Convert `_ = s.auditSvc.Log(...)` to fatal | Changes audit contract |
| Add `common/`, `helpers/`, or `base/` packages | Violates project structure |
| Rewrite entire service to reduce line count | Over-refactoring |

---

## Do Not

- Modify `CLAUDE.md`.
- Modify application behavior without explicit approval.
- Refactor and add features in the same change.
- Refactor and fix bugs in the same change (unless the bug is directly caused by what you are refactoring).
- Apply broad renaming across the codebase without explicit instruction.
- Touch unrelated files to make the diff "consistent".
