---
name: go-security
description: |
  Security review and hardening workflow for the HSK Vinh University Registration
  System backend. Invoke to audit existing code for security gaps and propose
  targeted fixes. Produces prioritized findings first; implements only after
  explicit approval for any change that alters token structure, CORS policy, or
  authentication behavior. Grounds every finding in actual code, not theory.
---

# go-security — Security Review and Hardening

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-security <target>
```

`<target>` may be a domain (`internal/auth/`), a concern (`"cors"`, `"jwt"`, `"sql injection"`), or `"full audit"` for a repository-wide pass.

This skill **produces findings first**. Implement a fix only after the user approves the specific finding. Do not apply broad security patches speculatively.

---

## Baseline Facts (verified against actual code)

| Item | Actual state |
|---|---|
| Auth scheme | JWT HMAC-SHA256, Bearer token, `golang-jwt/jwt/v5` |
| Token issuance | `auth.Service.issueToken` sets `sub`, `exp`, `iat` only — no `user_role` claim |
| Role enforcement | `middleware.RequireRole` reads `user_role` from gin context — set by `middleware.Claims.UserRole` |
| Claims parsing | `Claims` struct has `UserID` and `UserRole` fields; `UserRole` deserializes to `""` for tokens issued by current code |
| CORS | `cors.go` reflects the request `Origin` header verbatim with `Access-Control-Allow-Credentials: true` |
| Rate limiting | Global per-IP token bucket: 10 RPS / 20 burst (`RATE_LIMIT_RPS`, `RATE_LIMIT_BURST`); same limit applies to all routes including `/auth/login` |
| SQL construction | `$N` positional placeholders everywhere; `fmt.Sprintf` used only in `audit.List` for a dynamic `WHERE` clause — all values are still `$N` parameters |
| Password hashing | `bcrypt.DefaultCost` (10) — confirmed in `internal/utils/password.go` |
| Secrets in config | `DATABASE_URL` and `JWT_SECRET` loaded via `mustGetEnv` — `log.Fatalf` if absent |
| `.env` gitignore | `.env`, `.env.local`, `.env.*.local` all listed in `.gitignore` ✓ |
| Logging | Domain services have no `log.*` calls; only `[database]` and `[config]` packages log — no credential logging found |

---

## Phase 1 — JWT Role Gap (CRITICAL — verify before all else)

**Finding:** `issueToken` (in `internal/auth/service.go`) issues tokens with only `sub`, `exp`, and `iat`:
```go
claims := jwt.MapClaims{
    "sub": accountID,
    "exp": time.Now().Add(ttl).Unix(),
    "iat": time.Now().Unix(),
}
```

`middleware.Claims.UserRole` is never populated from the token, so `RequireRole("admin")` compares `""` against `"admin"` and always returns `403` for every user — making all admin endpoints unreachable.

**Impact:** All routes protected by `RequireRole` are effectively locked out for all users.

**Before proposing a fix:**
1. Read `internal/auth/service.go` `issueToken` function in full.
2. Read `internal/middleware/auth.go` `Claims` struct and `JWTAuth` function.
3. Confirm the gap by checking: what claim name does `Claims.UserRole` map to?

The `Claims` struct embeds `jwt.RegisteredClaims` and declares `UserRole string \`json:"user_role"\``. The issuer never writes `"user_role"` into the token. This is the gap.

**Proposed fix (requires approval — changes token structure):**

In `auth.Service.issueToken`, add the role. This requires reading the account's role from the DB first, or passing it as a parameter. The minimal-change approach: add a `role string` parameter to `issueToken` and embed it:

```go
func (s *Service) issueToken(accountID int64, role string, ttl time.Duration) (string, error) {
    claims := jwt.MapClaims{
        "sub":       accountID,
        "user_role": role,
        "exp":       time.Now().Add(ttl).Unix(),
        "iat":       time.Now().Unix(),
    }
    return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}
```

Update `Login` to pass the account's role code (already available from the accounts → roles join or by reading `account_role_id`).

**Stop and ask the user before implementing** — this changes token structure, breaking all existing issued tokens on deployment.

---

## Phase 2 — CORS Reflection (HIGH — production-blocking)

**Finding:** `cors.go` reflects the `Origin` request header verbatim:
```go
origin := c.GetHeader("Origin")
if origin == "" {
    origin = "*"
}
c.Header("Access-Control-Allow-Origin", origin)
c.Header("Access-Control-Allow-Credentials", "true")
```

`Allow-Credentials: true` combined with a reflected origin effectively allows any origin to make credentialed requests. This is equivalent to `*` with credentials, which browsers reject per the CORS spec — but it accepts any explicitly-stated origin instead.

**Proposed fix (requires approval — changes CORS policy):**

Replace with an explicit allow-list controlled by config:

```go
// In config.go, add:
AllowedOrigins []string  // loaded from CORS_ALLOWED_ORIGINS env var (comma-separated)

// In cors.go, replace reflection with:
func CORS(allowedOrigins []string) gin.HandlerFunc {
    allowed := make(map[string]struct{}, len(allowedOrigins))
    for _, o := range allowedOrigins { allowed[o] = struct{}{} }
    return func(c *gin.Context) {
        origin := c.GetHeader("Origin")
        if _, ok := allowed[origin]; ok {
            c.Header("Access-Control-Allow-Origin", origin)
            c.Header("Access-Control-Allow-Credentials", "true")
        }
        // ... existing headers ...
    }
}
```

Add `CORS_ALLOWED_ORIGINS=https://app.example.com` to `.env.example`.

For **development only**, allow `"http://localhost:*"` via a separate dev origin list.

---

## Phase 3 — Login Brute-Force Surface (MEDIUM)

**Finding:** The rate limiter is applied globally to all routes at `engine.Use(...)` level. There is no tighter limit on `/api/v1/auth/login` specifically.

**Current state:** 10 RPS global per IP. An attacker can exhaust 10 login attempts per second before hitting a 429. With a 20-token burst, they can fire 20 attempts instantly.

**Assessment:** For a university exam registration system this is an accepted risk if:
- The account lockout path (`AccountStatusLocked`) is accessible to admins.
- bcrypt cost 10 makes each check ~100ms, limiting effective throughput further.

**Proposed improvement (if user approves):** Apply a separate, tighter rate limit group to auth routes in `auth/route.go`:

```go
// In auth/route.go RegisterRoutes, create a tighter limiter for auth:
authLimiter := middleware.NewRateLimiter(3, 5) // 3 RPS / 5 burst
authGroup.Use(authLimiter.RateLimit())
```

This requires passing a separate `*middleware.RateLimiter` into `RegisterRoutes` — a constructor change.

---

## Phase 4 — SQL Injection Surface (VERIFY)

Reference `go-database` Phase 2 for the full query pattern checklist — do not duplicate it here.

Specific check for this skill:

The only `fmt.Sprintf` usage in SQL is in `internal/audit/service.go` `List` function. Inspect it to confirm all values are still `$N` parameters (they are — `fmt.Sprintf` is only used to slot in the `WHERE` clause fragment and `LIMIT`/`OFFSET` indexes, not user-supplied values). This is **safe**.

Run this check for any new service:
```bash
grep -rn "fmt.Sprintf" internal/
# Then verify each Sprintf in a SQL context uses $N for all user input
```

---

## Phase 5 — Secrets Handling Audit (LOW — currently clean)

Verify the following checklist. All currently pass:

- [x] `.env` is in `.gitignore`.
- [x] `JWT_SECRET` and `DATABASE_URL` are never printed with `log.*`.
- [x] `bcrypt.DefaultCost` used — confirmed in `internal/utils/password.go`.
- [x] No domain service has `log.*` calls that could leak credentials.
- [x] `.env.example` uses a placeholder for `JWT_SECRET`: `your-super-secret-jwt-key-change-in-production`.

**Ongoing rule:** After every `go-feature` run that adds a service, run:
```bash
grep -rn "log\." internal/ | grep -i "token\|password\|secret\|jwt\|hash\|credential"
```
A clean result is required before merge.

---

## Phase 6 — Dependency Vulnerability Scanning (DEV TOOL — no new runtime dep)

`govulncheck` is a static analysis dev tool — it requires no new `go.mod` dependency and does not run at application runtime.

Install once:
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Add to verification workflow (alongside `go vet`):
```bash
govulncheck ./...
```

Add to CI pipeline (`go-deploy` Phase 3 covers this integration).

Run against the current module:
```bash
govulncheck ./...
```

Report any `CRITICAL` or `HIGH` findings and evaluate whether they affect the actual code paths in use (govulncheck traces call graphs).

---

## Verification Commands

```bash
gofmt -l ./...
go vet ./...
govulncheck ./...    # install once: go install golang.org/x/vuln/cmd/govulncheck@latest
go build ./...
go test -race ./...
```

---

## Output Format

```
## Finding: <title>
Severity: CRITICAL | HIGH | MEDIUM | LOW
Location: <file:line>
Evidence: <what the code actually does>
Impact: <concrete consequence>
Proposed fix: <specific, minimal change>
Requires approval: yes — because <reason>
```

---

## Do Not

- Propose OAuth, session-based auth, or any auth mechanism outside the existing JWT/HMAC-SHA256 scheme (CLAUDE.md §28).
- Introduce `golang.org/x/oauth2`, `gorilla/sessions`, or any auth library not already in `go.mod`.
- Add Redis for rate limiting or session storage (CLAUDE.md §28 — Redis is explicitly absent).
- Add structured logging (zerolog/zap/slog) to implement security audit trails — use the existing `log` package and `audit.Service` patterns (CLAUDE.md §19, §28).
- Implement any security fix without first presenting findings and obtaining explicit approval for changes that affect token structure, CORS policy, or API behavior.
