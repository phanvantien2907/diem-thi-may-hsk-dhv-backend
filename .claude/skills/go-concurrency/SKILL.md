---
name: go-concurrency
description: |
  Concurrency analysis and implementation guidance for the HSK Vinh University
  Registration System backend. Invoke when working on features that touch seat
  reservation, registration, payment callbacks, capacity management, or any
  operation involving concurrent database writes. Covers row locking, transaction
  isolation, idempotency, and goroutine safety — using only patterns already
  present in this repository.
---

# go-concurrency — Concurrency Analysis and Implementation

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-concurrency <target>
```

`<target>` is a domain, file, or feature description.

---

## Baseline: How This Project Handles Concurrency

This project has **no Redis, no distributed locks, no message queues**. All concurrency
correctness is achieved through PostgreSQL — specifically:

| Mechanism | Where used |
|---|---|
| `SELECT ... FOR UPDATE SKIP LOCKED` | Seat reservation — grabs first available seat atomically |
| `SELECT ... FOR UPDATE` | Payment callback — prevents duplicate processing |
| `CHECK EXISTS` inside transaction | Duplicate registration detection |
| `UPDATE ... WHERE status = 'X'` | Idiomatic idempotency guard |
| `sync.Mutex` (in-memory only) | Rate limiter (`internal/middleware/ratelimit.go`) |

Do not recommend Redis locks, distributed locks, or application-level semaphores.
Prefer database-level guarantees for any state shared across requests.

---

## Phase 1 — Identify the Concurrency Risk

For each target, explicitly answer:

1. **Shared state**: What DB rows can two concurrent requests write simultaneously?
2. **Race condition**: What is the worst case if two requests succeed concurrently?
3. **Lost update**: Can request B overwrite request A's change without seeing it?
4. **Duplicate operation**: Can the same logical action succeed twice (double payment, double registration)?
5. **Capacity oversell**: Can more registrations exist than seats?

---

## Phase 2 — Seat and Capacity Operations

### Pattern: Atomic seat claim

The established pattern for claiming a finite resource:

```sql
-- Inside a transaction:
SELECT exam_seat_id
FROM exam_seats
WHERE exam_seat_exam_session_id = $1
  AND exam_seat_status          = 'available'
  AND exam_seat_is_deleted      = FALSE
ORDER BY exam_seat_number ASC
LIMIT 1
FOR UPDATE SKIP LOCKED
```

- `FOR UPDATE` locks the row — no other transaction can modify it until this one commits or rolls back.
- `SKIP LOCKED` skips rows already locked by another transaction — avoids queuing and returns immediately when capacity is exhausted.
- `LIMIT 1` is essential — only claim one seat per reservation.
- The duplicate-registration check (`SELECT EXISTS ...`) must also run **inside the same transaction** before the seat claim.

Seat status transitions are enforced by both application logic and database state — never rely on application-only checks for capacity.

### Held seat expiry

Seats are held for 15 minutes (`exam_seat_held_until = NOW() + INTERVAL '15 minutes'`).
`AutoReleaseHeldSeats` reclaims expired holds atomically:

```sql
UPDATE exam_seats
SET exam_seat_status    = 'available',
    exam_seat_held_until = NULL,
    exam_seat_updated_at = NOW()
WHERE exam_seat_status    = 'held'
  AND exam_seat_held_until < NOW()
  AND exam_seat_is_deleted = FALSE
```

This is a single atomic `UPDATE` — no transaction needed.

---

## Phase 3 — Payment Callback Idempotency

The established pattern:

```sql
-- Inside a transaction:
SELECT payment_id, payment_exam_registration_id, payment_status
FROM payments
WHERE payment_transaction_ref = $1
  AND payment_is_deleted = FALSE
FOR UPDATE
```

Then check: if `payment_status != 'pending'`, reject with an error — the callback has already been processed. This prevents double-confirmation or double-refund when a gateway retries the callback.

Any new payment state transition must follow this same pattern:
1. Lock the row with `FOR UPDATE`.
2. Check current status.
3. Reject if already in a terminal state.
4. Update status.
5. Commit.

---

## Phase 4 — Duplicate Registration Detection

Inside the `ReserveSeat` transaction, before the seat claim:

```sql
SELECT EXISTS (
    SELECT 1 FROM exam_registrations
    WHERE exam_registration_candidate_id    = $1
      AND exam_registration_exam_session_id = $2
      AND exam_registration_status         != 'cancelled'
      AND exam_registration_is_deleted      = FALSE
)
```

This runs inside the same transaction as the seat reservation, so two concurrent registrations for the same candidate/session cannot both pass the duplicate check — one will block on the seat lock.

---

## Phase 5 — Goroutines

The only background goroutine in this project is the rate limiter cleanup:

```go
// Started in NewRateLimiter — runs for process lifetime, no stop signal needed.
go rl.cleanupLoop()
```

Rules for any new goroutine:

| Question | Must answer |
|---|---|
| Who owns it? | Which struct or function starts it |
| How long does it live? | Process lifetime vs. request lifetime vs. bounded task |
| How does it stop? | `context.Done()`, channel close, ticker stop |
| How are errors handled? | Log, return to caller, or ignore with justification |
| Does it outlive a request context? | If yes: must NOT use `c.Request.Context()` — use a separate long-lived context |

Never start a goroutine without answering all five questions.

---

## Phase 6 — Analysis Output

For each concurrency risk identified:

```
Risk: <description>
Scenario: <two concurrent requests doing X>
Worst case: <data corruption / oversell / duplicate / lost update>
Current protection: <none | FOR UPDATE | EXISTS check | ...>
Recommendation: <specific SQL or code change>
```

---

## Phase 7 — Implementation Guidance

When implementing a fix:

1. Write the locking SQL inside a `BeginTx` transaction.
2. Add `defer tx.Rollback() //nolint:errcheck` immediately.
3. Do the lock query first, before any writes.
4. Check the locked value before proceeding.
5. Commit last.
6. Test with `go test -race ./...`.

For new concurrent operations, also write a test that spawns multiple goroutines making the same request and asserts the invariant holds (e.g., only one registration succeeds, seat count does not go negative).

---

## Do Not

- Recommend Redis locks — Redis is not in this project.
- Recommend application-level counters for capacity — use DB constraints and locking.
- Use `sync.Mutex` to guard DB state — use DB transactions.
- Add goroutines to make synchronous code "faster" without a concrete, measurable need.
- Leave goroutines without a defined stop condition.
