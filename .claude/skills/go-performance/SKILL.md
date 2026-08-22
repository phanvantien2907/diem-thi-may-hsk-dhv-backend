---
name: go-performance
description: |
  Performance analysis and improvement workflow for the HSK Vinh University
  Registration System backend. Complements go-review (which flags symptoms) by
  providing measurement-driven workflows: connection pool verification, N+1
  detection via EXPLAIN ANALYZE, pprof profiling for local use only, and
  benchmark patterns for the concurrency-critical ReserveSeat and ProcessCallback
  paths. Never optimizes without evidence. Defers to go-database Phase 5 for
  index recommendations.
---

# go-performance — Performance Analysis and Improvement

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-performance <target>
```

`<target>` may be a domain (`internal/registration/`), a specific operation (`"seat reservation"`, `"payment callback"`), or `"connection pool"`.

This skill **measures before changing**. No optimization is applied without evidence from profiling, `EXPLAIN ANALYZE`, or benchmark output. Reference `go-database` Phase 5 for index strategy — do not duplicate it here.

---

## Baseline Facts (verified against actual code)

| Concern | Actual state |
|---|---|
| DB pool `MaxOpenConns` | Configured: default `25` (env `DB_MAX_OPEN_CONNS`), set in `database.Connect` via `db.SetMaxOpenConns` |
| DB pool `MaxIdleConns` | Configured: default `5` (env `DB_MAX_IDLE_CONNS`), set via `db.SetMaxIdleConns` |
| DB pool `ConnMaxLifetime` | Configured: default `5m` (env `DB_CONN_MAX_LIFETIME`), set via `db.SetConnMaxLifetime` |
| `ConnMaxIdleTime` | **Not set** — `sql.DB` default is no limit on idle connection lifetime |
| `pprof` | **Not registered anywhere** — safe |
| Benchmarks | **None exist** — no `*_test.go` files at all in the repo |
| Query timing | **Not instrumented** — no per-query duration measurement |
| N+1 queries | No detected loops calling DB inside row iteration in existing code |

---

## Phase 1 — Connection Pool Verification and Tuning

**Current pool defaults (from `config.go` and `postgres.go`):**

```
DB_MAX_OPEN_CONNS=25   → db.SetMaxOpenConns(25)
DB_MAX_IDLE_CONNS=5    → db.SetMaxIdleConns(5)
DB_CONN_MAX_LIFETIME=5m → db.SetConnMaxLifetime(5m)
ConnMaxIdleTime         → NOT SET (uses sql.DB default: unlimited)
```

**Sizing guidance for this deployment target (single-instance Docker, PostgreSQL 16):**

| Parameter | Current default | Recommendation |
|---|---|---|
| `DB_MAX_OPEN_CONNS` | 25 | Reasonable for a single Go process; ensure PostgreSQL `max_connections` is at least 50 (default is 100) |
| `DB_MAX_IDLE_CONNS` | 5 | Acceptable; increase to 10 if connection establishment latency is observed |
| `DB_CONN_MAX_LIFETIME` | 5m | Correct — prevents stale connections after PostgreSQL restarts |
| `ConnMaxIdleTime` | Not set | Add `db.SetConnMaxIdleTime(1 * time.Minute)` in `postgres.go` to recycle long-idle connections |

**To add `ConnMaxIdleTime`** (a one-line change to `postgres.go`, requires no config change, safe to implement):

```go
// In database/postgres.go Connect(), after existing Set* calls:
db.SetConnMaxLifetime(opts.ConnMaxLifetime)
db.SetConnMaxIdleTime(1 * time.Minute)  // add this
```

Expose via config if the team wants to tune it:
```go
// In config.go:
DBConnMaxIdleTime time.Duration  // env: DB_CONN_MAX_IDLE_TIME, default: "1m"
```

**Verify pool saturation under load:**
```sql
-- Run against PostgreSQL while load is applied:
SELECT count(*), state
FROM pg_stat_activity
WHERE datname = 'hsk_vinh'
GROUP BY state;
```

If `active` approaches `DB_MAX_OPEN_CONNS`, increase the pool or optimize slow queries first.

---

## Phase 2 — N+1 Query Detection

**Theory:** N+1 occurs when code issues 1 query to fetch N rows, then 1 query per row inside a loop.

**Check in existing code:**
```bash
# Find any DB call inside a for/range block
grep -n "QueryRowContext\|QueryContext\|ExecContext" internal/*/service.go \
  | while read line; do
      file=$(echo "$line" | cut -d: -f1)
      lineno=$(echo "$line" | cut -d: -f2)
      # Check if this line is inside a rows.Next() loop
      context=$(sed -n "$((lineno-10)),$((lineno+2))p" "$file")
      echo "$context" | grep -q "rows.Next\|for.*range" && echo "POTENTIAL N+1: $file:$lineno"
    done
```

**Current assessment:** No N+1 detected in the existing service files. All list methods use a single `QueryContext` + scan loop.

**Watch for N+1 when adding features** that relate entities to multiple sub-resources (e.g., fetching a list of registrations and then per-registration fetching the exam session details in a loop).

**Correct pattern** — join at the SQL level, not in application code:
```sql
-- Instead of N queries:
SELECT r.*, s.exam_session_date, s.exam_session_shift
FROM exam_registrations r
JOIN exam_sessions s ON s.exam_session_id = r.exam_registration_exam_session_id
WHERE r.exam_registration_candidate_id = $1
  AND r.exam_registration_is_deleted = FALSE
```

---

## Phase 3 — EXPLAIN ANALYZE Workflow

Use `EXPLAIN ANALYZE` before recommending any index. Reference `go-database` Phase 5 for index decision criteria — do not duplicate it here.

**Step-by-step for a suspect query:**

1. Identify the query from a service file.
2. Connect to the development DB:
   ```bash
   docker compose exec postgres psql -U postgres -d hsk_vinh
   ```
3. Run `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)`:
   ```sql
   EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
   SELECT exam_seat_id FROM exam_seats
   WHERE exam_seat_exam_session_id = 1
     AND exam_seat_status = 'available'
     AND exam_seat_is_deleted = FALSE
   ORDER BY exam_seat_number ASC LIMIT 1
   FOR UPDATE SKIP LOCKED;
   ```
4. Look for:
   - `Seq Scan` on a large table → candidate for an index.
   - `Index Scan` → already indexed correctly.
   - `rows=N` (estimated) vs `actual rows=N` (actual) — large divergence indicates stale statistics; run `ANALYZE <table>`.
   - `Buffers: shared hit=N read=N` — high `read` means data not cached.

**Critical queries to benchmark for this project:**

| Query | File | Risk |
|---|---|---|
| `SELECT ... FOR UPDATE SKIP LOCKED` on `exam_seats` | `registration/service.go:54` | High concurrency |
| `SELECT EXISTS (...)` duplicate check | `registration/service.go:38` | Called inside transaction |
| `UPDATE payments ... WHERE payment_transaction_ref=$1 FOR UPDATE` | `payment/service.go:103` | Callback idempotency |
| `SELECT COUNT(*) FROM audit_logs WHERE ...` | `audit/service.go:118` | Unbounded count |

---

## Phase 4 — pprof Profiling (LOCAL ONLY)

**Current state:** `pprof` is not registered anywhere in the project. This is correct — never expose `net/http/pprof` on a production-bound listener.

**For local profiling only**, add temporarily to `main.go` and remove before committing:

```go
// TEMPORARY — never commit to main or deploy to production:
import _ "net/http/pprof"
go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
```

This binds pprof only to `localhost:6060` — not to the application port.

**Profile the seat reservation path under load:**
```bash
# Terminal 1: run the app with pprof
# Terminal 2: generate load
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Or capture a heap profile:
go tool pprof http://localhost:6060/debug/pprof/heap
```

**Reading pprof output — key patterns to look for:**

| Pattern | Meaning |
|---|---|
| `database/sql.(*DB).conn` high CPU | Connection pool exhaustion — increase `MaxOpenConns` or optimize queries |
| `encoding/json.Marshal` in hot path | JSON serialization overhead — consider pre-marshaling audit payloads |
| `sync.(*Mutex).Lock` contention | Rate limiter contention — look at `middleware/ratelimit.go` visitor map |
| `runtime.mallocgc` high allocs | Excessive allocation — look for slices grown in loops |

**Remove pprof import before any commit:**
```bash
grep -rn "net/http/pprof" .
# Must return no results
```

---

## Phase 5 — Benchmark Patterns for Critical Paths

The project has no existing tests or benchmarks. When writing performance benchmarks, follow the standard Go pattern and place them in `*_test.go` files in the same package.

**Template for `ReserveSeat` benchmark (requires a test DB):**

```go
// internal/registration/service_bench_test.go
package registration

import (
    "context"
    "testing"
)

func BenchmarkReserveSeat(b *testing.B) {
    // Setup: requires a real test DB with at least one available seat
    svc := // ... setup service with test DB ...
    req := request.ReserveSeatRequestDTO{CandidateID: 1, ExamSessionID: 1}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = svc.ReserveSeat(context.Background(), req)
    }
}
```

Run:
```bash
go test -bench=BenchmarkReserveSeat -benchmem -count=5 ./internal/registration/
```

**Template for concurrent correctness benchmark:**
```bash
go test -bench=BenchmarkReserveSeat -cpu=1,2,4,8 -benchmem ./internal/registration/
```

Increasing `-cpu` while holding seat count constant should produce increasing failure rates (correct behavior — most goroutines should fail after one seat is taken).

---

## Verification Commands

```bash
go build ./...
go test -bench=./... -benchmem ./...
go test -race ./...

# Pool check (while app is running):
# psql: SELECT count(*), state FROM pg_stat_activity WHERE datname='hsk_vinh' GROUP BY state;

# pprof cleanup check (before commit):
grep -rn "net/http/pprof" .
```

---

## Do Not

- Add a caching layer (Redis, memcached, in-memory) without explicit approval (CLAUDE.md §28 — Redis is absent; in-memory cache not part of the architecture).
- Optimize any path without running `EXPLAIN ANALYZE` or a benchmark first — evidence required.
- Expose `net/http/pprof` on the production port — local `localhost:6060` only, and remove before committing.
- Add a query builder or ORM to reduce SQL repetition as a "performance" improvement (CLAUDE.md §28 — no ORM).
- Add speculative indexes — reference `go-database` Phase 5 for index policy.
- Introduce goroutines as an optimization without establishing ownership, lifecycle, and cancellation (CLAUDE.md §17).
