---
name: go-observability
description: |
  Observability review and improvement workflow for the HSK Vinh University
  Registration System backend. Operates strictly within the existing stdlib log
  package (CLAUDE.md §19). Produces findings first. Covers log prefix consistency,
  request correlation IDs, sensitive-data audit, query duration visibility, and
  a minimal stdlib-only slow-query pattern. Any proposal to add metrics or tracing
  requires explicit user approval before any code is written.
---

# go-observability — Observability Review and Improvement

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-observability <target>
```

`<target>` may be a domain (`internal/registration/`), a concern (`"log prefixes"`, `"slow queries"`, `"correlation id"`), or `"full audit"`.

This skill **produces findings first**. Implement changes only after the user approves each item. Changes involving new dependencies require **explicit user approval** before any code is written.

---

## Baseline Facts (verified against actual code)

| Item | Actual state |
|---|---|
| Logging library | Standard library `log` package only — no zerolog, zap, or slog |
| HTTP request logging | `middleware/logger.go` — Gin's `LoggerWithFormatter`; logs timestamp, IP, method, path, status, latency |
| Log fields logged | `time.DateTime \| ClientIP \| Method Path \| StatusCode \| Latency` |
| Request correlation ID | **Not present** — no `X-Request-ID` generated or propagated through log lines |
| Domain service logging | **No `log.*` calls** in any domain service (`auth`, `registration`, `payment`, etc.) |
| Package prefix convention | `[database]` and `[config]` use the `[package]` prefix; `[main]` uses it in `cmd/api/main.go` |
| Slow query logging | **Not present** — no query duration is captured in service code |
| Metrics | **None** — no Prometheus, StatsD, or equivalent |
| Tracing | **None** — no OpenTelemetry or similar |

---

## Phase 1 — Log Prefix Consistency Audit

The convention defined in CLAUDE.md §19 is: prefix log messages with `[package]`.

**Verified compliant:**
- `cmd/api/main.go` — uses `[main]`
- `internal/database/postgres.go` — uses `[database]`
- `internal/config/config.go` — uses `[config]`

**Domain services:** No `log.*` calls exist in any domain service. This is intentional — domain errors surface through return values, not logs.

**Action:** Verify that any new `log.*` call added to a domain package follows the `[package]` convention:

```go
log.Printf("[registration] auto-release: released %d held seats", n)
log.Printf("[auth] login failed for unknown user (timing-safe)")
```

Run this check after any `go-feature` run that adds service-level logging:
```bash
grep -rn "log\." internal/ | grep -v "_test.go"
# Every log.* call must have a [package] prefix
```

---

## Phase 2 — Request Correlation ID (MEDIUM)

**Finding:** `middleware/logger.go` logs requests without a correlation ID. When multiple concurrent requests fail, log lines cannot be correlated to a specific request.

The `cors.go` file already sets `Access-Control-Expose-Headers: X-Request-ID`, indicating this was planned but not implemented.

**Proposed fix (stdlib only — no new dependency):**

Add a correlation ID middleware that generates a unique ID per request using `math/rand` or a simple atomic counter, attaches it to the gin context, and includes it in the log line and response header.

```go
// In internal/middleware/requestid.go (new file, no new dependencies):
package middleware

import (
    "fmt"
    "math/rand"
    "github.com/gin-gonic/gin"
)

const ContextKeyRequestID = "request_id"

func RequestID() gin.HandlerFunc {
    return func(c *gin.Context) {
        id := c.GetHeader("X-Request-ID")
        if id == "" {
            id = fmt.Sprintf("%016x", rand.Int63())
        }
        c.Set(ContextKeyRequestID, id)
        c.Header("X-Request-ID", id)
        c.Next()
    }
}
```

Update `middleware/logger.go` to include the request ID in its log line.

Update global middleware order in `main.go` to add `RequestID()` before `Logger()`.

**Stop and ask the user before implementing** — this adds a new middleware file and changes the log format.

---

## Phase 3 — Sensitive Data in Logs Audit (REQUIRED after every go-feature)

The prohibition from CLAUDE.md §19 — "never log passwords, tokens, or sensitive payment data" — must be enforced.

Run after every feature addition:

```bash
grep -rn "log\." internal/ | grep -i "token\|password\|secret\|jwt\|hash\|card\|cvv\|amount"
```

Expected: no results.

If a result is found:

1. Locate the log call.
2. Determine whether the sensitive value is directly in the format string or in a struct being `%v`-printed.
3. Remove or redact the value. Never `fmt.Sprintf` a password or token into a log line.

**Example safe pattern:**
```go
// OK — logs the action, not the value:
log.Printf("[auth] password reset requested for account %d", accountID)

// Not OK — logs the token:
log.Printf("[auth] issued token: %s", accessToken)
```

---

## Phase 4 — Slow Query Visibility (LOW)

**Finding:** No query duration is logged anywhere. When a slow `ReserveSeat` or `ProcessCallback` query degrades throughput, there is no per-query signal in the logs.

**Proposed pattern (stdlib only — no new dependency):**

Wrap slow queries with a time check inside the service function, logging only when a threshold is exceeded:

```go
// Minimal slow-query helper — add to internal/utils/ if used in 3+ domains:
start := time.Now()
// ... db query ...
if d := time.Since(start); d > 500*time.Millisecond {
    log.Printf("[registration] slow query: reserve_seat took %s", d)
}
```

Apply this pattern selectively to the two most critical paths:
- `registration.ReserveSeat` — the seat lock query
- `payment.ProcessCallback` — the payment lock + update

**Do not add this to every query** — only where latency is operationally significant.

**Stop and ask before adding** — the user must approve adding timing code to these sensitive concurrency paths.

---

## Phase 5 — Metrics and Tracing Boundary (EXPLICIT APPROVAL REQUIRED)

Any proposal to add Prometheus metrics, StatsD, or OpenTelemetry tracing introduces new runtime dependencies not present in the current stack.

Per CLAUDE.md §28, this boundary exists intentionally. Do not implement any metrics or tracing without explicit user approval.

If the user approves, the correct integration point for Prometheus would be:
- A new `metrics` middleware added to `engine.Use(...)` in `main.go`.
- Counters/histograms for: request count by endpoint, request duration, DB query duration, seat reservation success/failure rate, payment callback idempotency hits.

**Default behavior without approval:** Report the gap, explain the benefit, and wait.

---

## Verification Commands

```bash
# Log prefix audit
grep -rn "log\." internal/ | grep -v "_test.go"

# Sensitive data audit  
grep -rn "log\." internal/ | grep -i "token\|password\|secret\|jwt\|hash"

# Verify no new logging library imported
grep -rn "zerolog\|uber-go/zap\|rs/zerolog\|slog" go.mod go.sum
```

---

## Do Not

- Import or propose zerolog, zap, or `log/slog` without explicit user approval (CLAUDE.md §19, §28).
- Add Prometheus (`prometheus/client_golang`), OpenTelemetry, or any metrics/tracing library without explicit approval (CLAUDE.md §28 — not part of the current stack).
- Add Redis for distributed tracing or log aggregation (CLAUDE.md §28).
- Log passwords, tokens, payment data, or JWT secrets — ever (CLAUDE.md §19).
- Remove the `[package]` prefix convention when adding new `log.*` calls.
- Implement a correlation ID using a third-party `uuid` package — use `math/rand` or an atomic counter only.
