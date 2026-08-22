---
name: go-resilience
description: |
  Production-readiness review for the HSK Vinh University Registration System
  backend. Inspects and reports on graceful shutdown, health endpoints, HTTP and
  DB timeouts, and startup failure handling — then implements missing items using
  only stdlib and Gin (no new dependencies). Each finding states whether it already
  exists in the current codebase before proposing any change.
---

# go-resilience — Production-Readiness Review

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-resilience <target>
```

`<target>` may be a concern (`"graceful shutdown"`, `"health checks"`, `"timeouts"`, `"startup"`) or `"full audit"`.

This skill **reports current state first**, then implements only what is genuinely missing and only after the user confirms the scope. Uses stdlib and Gin only — no new dependencies.

---

## Baseline Facts (verified against actual code)

| Concern | Actual state in this repo |
|---|---|
| Graceful shutdown | **EXISTS** — `signal.NotifyContext(SIGINT, SIGTERM)` + `srv.Shutdown(30s timeout)` in `cmd/api/main.go:115-134` |
| HTTP `ReadTimeout` | **SET** — `15 * time.Second` (`main.go:109`) |
| HTTP `WriteTimeout` | **SET** — `15 * time.Second` (`main.go:110`) |
| HTTP `IdleTimeout` | **SET** — `60 * time.Second` (`main.go:111`) |
| Liveness endpoint | **EXISTS** — `GET /health` registered directly on `engine` in `main.go:82-89`; returns `{"success":true,"data":{"status":"ok"}}` — no DB check |
| Readiness endpoint | **ABSENT** — no separate `/readyz` or `/healthz`; the existing `/health` does not ping the DB |
| DB connection verify at startup | **EXISTS** — `database.Connect` calls `db.Ping()` on line 31 of `postgres.go`; `main.go` calls `log.Fatalf` if it fails |
| DB `context.WithTimeout` in services | **ABSENT** — services use `c.Request.Context()` directly (correct per CLAUDE.md §16); no additional per-query timeout layer |
| Docker health check | **EXISTS** in `docker-compose.yml` — `pg_isready` on postgres; API depends on `service_healthy` postgres |
| `AutoReleaseHeldSeats` retry/backoff | **ABSENT** — no periodic invocation of this function is wired up anywhere in `main.go` |

---

## Phase 1 — Graceful Shutdown (ALREADY IMPLEMENTED — verify only)

Graceful shutdown is fully implemented. Verify it remains correct whenever `main.go` is modified:

1. `signal.NotifyContext` catches `SIGINT` and `SIGTERM`. ✓
2. The goroutine running `ListenAndServe` logs `http.ErrServerClosed` without fataling. ✓
3. `srv.Shutdown(30s)` drains in-flight requests before exit. ✓
4. `database.Close(db)` is deferred to close the pool after shutdown. ✓

**Check after any `go-feature` run that touches `main.go`:**
```bash
grep -n "Shutdown\|NotifyContext\|SIGTERM\|SIGINT" cmd/api/main.go
# All four must be present
```

---

## Phase 2 — Health Endpoints (PARTIAL — readiness missing)

**Current state:** `GET /health` exists on the root engine (not under `/api/v1`). It is a liveness check only — it returns `ok` without verifying the DB connection.

**Gap:** No readiness endpoint that tests whether the application can actually serve traffic (DB reachable, dependencies healthy).

**Proposed additions (no new dependency):**

Add `/readyz` alongside the existing `/health` in `main.go`, using `db.PingContext`:

```go
// Liveness — already exists, verify shape is correct:
engine.GET("/health", func(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    gin.H{"status": "ok"},
        "error":   nil,
        "meta":    nil,
    })
})

// Readiness — new, add after the health check:
engine.GET("/readyz", func(c *gin.Context) {
    ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{
            "success": false,
            "data":    nil,
            "error":   "database unavailable",
            "meta":    nil,
        })
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data":    gin.H{"status": "ready"},
        "error":   nil,
        "meta":    nil,
    })
})
```

**Important:** Both `/health` and `/readyz` must remain on the root `engine`, not under `/api/v1`, so load balancers and container orchestrators can reach them without auth headers.

**Note on `context.WithTimeout` here:** This is a health endpoint concern, not a request handler concern. The 2-second DB ping timeout is deliberately scoped to the readiness check. This does not conflict with CLAUDE.md §16 — domain service methods still use `c.Request.Context()` directly.

**Stop and ask before implementing** — user must confirm they want `/readyz` added.

---

## Phase 3 — Per-Query DB Timeouts (LOW — evaluate before adding)

**Current state:** Domain services use `c.Request.Context()` throughout, which is correct per CLAUDE.md §16. The HTTP `WriteTimeout` of 15s provides an outer bound. No per-query timeout is layered on top.

**Assessment for this project:**

| Operation | Typical duration | Timeout risk |
|---|---|---|
| `ReserveSeat` (with lock) | < 100ms under normal load | Low — but can queue under contention |
| `ProcessCallback` | < 50ms | Low |
| `audit.List` (dynamic WHERE) | Variable with filter size | Medium — no LIMIT on large result sets without pagination params |

**Recommendation:** Do not add `context.WithTimeout` to individual service methods by default. The request context already carries the HTTP server's `WriteTimeout`. Adding per-query timeouts risks cutting short in-flight transactions mid-operation, leaving rows locked.

**Exception:** If `AutoReleaseHeldSeats` is invoked periodically (see Phase 4), it must use its own `context.WithTimeout`, not a request context.

---

## Phase 4 — AutoReleaseHeldSeats Invocation Gap (MEDIUM)

**Finding:** `registration.Service.AutoReleaseHeldSeats` exists and is correct but is **never called**. There is no periodic invocation in `main.go` or anywhere else. Held seats expire based on `exam_seat_held_until < NOW()`, but the `UPDATE` that resets them to `available` is never executed automatically.

**Impact:** Held seats with expired holds remain in `held` state indefinitely, reducing visible capacity. They can only be recovered by manual SQL or a future request that happens to trigger the cleanup.

**Proposed fix (stdlib only — `time.Ticker` goroutine):**

In `main.go`, after the server goroutine is started, add:

```go
// ── Auto-release expired seat holds ────────────────────────────────────────
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            n, err := regSvc.AutoReleaseHeldSeats(releaseCtx)
            cancel()
            if err != nil {
                log.Printf("[main] auto-release seats: %v", err)
            } else if n > 0 {
                log.Printf("[main] auto-release seats: released %d held seats", n)
            }
        case <-ctx.Done():
            return
        }
    }
}()
```

**Note:** This goroutine uses `context.Background()` with an explicit 10-second timeout — not a request context — because it runs outside of any HTTP request. It respects the `ctx.Done()` signal for graceful shutdown.

**Stop and ask before implementing** — user must confirm the 5-minute interval and confirm this goroutine should be added to `main.go`.

---

## Phase 5 — Startup Failure Behavior (ALREADY CORRECT)

**Current state:**

1. `config.Load()` → `log.Fatalf` if `DATABASE_URL` or `JWT_SECRET` are absent. ✓
2. `database.Connect()` → `db.Ping()` at startup; returns error → `log.Fatalf("[main] database: %v", err)`. ✓
3. Docker Compose `depends_on: postgres: condition: service_healthy` prevents the API container from starting before the DB is ready. ✓

**No action needed.** The startup fast-fail path is correct.

**Verify after `go-feature` runs that add new `mustGetEnv` calls:**
```bash
grep -n "mustGetEnv\|log.Fatalf" internal/config/config.go
```

---

## Verification Commands

```bash
# Confirm graceful shutdown intact
grep -n "Shutdown\|NotifyContext\|SIGTERM\|SIGINT" cmd/api/main.go

# Confirm HTTP timeouts set
grep -n "ReadTimeout\|WriteTimeout\|IdleTimeout" cmd/api/main.go

# Confirm health endpoints
grep -n "health\|readyz" cmd/api/main.go

# Build check
go build ./...
go test -race ./...
```

---

## Do Not

- Add external health check libraries (CLAUDE.md §28 — no new deps unless explicitly approved).
- Add Redis for circuit breakers or distributed rate limiting (CLAUDE.md §28 — Redis is absent).
- Add `hystrix-go` or any circuit breaker library without explicit approval.
- Wrap every service DB call with `context.WithTimeout` indiscriminately — this risks mid-transaction timeouts and connection leaks.
- Move `/health` or `/readyz` under `/api/v1` — they must remain at the root for infrastructure tooling.
- Remove the existing graceful shutdown logic — it is already correct and production-ready.
