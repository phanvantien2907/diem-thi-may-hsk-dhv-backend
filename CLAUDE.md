# CLAUDE.md — Project Engineering Constitution
# HSK Vinh University Registration System — Golang Backend

This document is the authoritative engineering reference for this codebase.
Every future agent that implements, tests, reviews, refactors, or modifies this backend **must** read and follow these rules.

The existing implementation is the source of truth. Rules here describe what the code actually does, not an ideal. Follow these conventions faithfully.

---

## 1. Technology Stack

| Concern | Technology |
|---|---|
| Language | Go 1.23 |
| HTTP framework | Gin (`github.com/gin-gonic/gin v1.10.0`) |
| Database | PostgreSQL 16 |
| Database driver | `github.com/lib/pq v1.12.3` via `database/sql` |
| ORM / query builder | None — raw SQL with `database/sql` |
| Authentication | JWT (`github.com/golang-jwt/jwt/v5 v5.2.1`), HMAC-SHA256, Bearer token |
| Password hashing | bcrypt (`golang.org/x/crypto`) |
| Validation | `github.com/go-playground/validator/v10 v10.22.0` via `utils.Validate.Struct()` |
| Configuration | `github.com/joho/godotenv v1.5.1` + `os.LookupEnv` |
| Logging | Standard library `log` package |
| Migration tool | Plain SQL files applied by `docker-entrypoint-initdb.d` (Postgres init) |
| Rate limiting | Custom in-memory token bucket (`internal/middleware/ratelimit.go`) |
| Redis | **Not used** — commented out in `docker-compose.yml` |
| Dependency injection | Manual constructor wiring in `cmd/api/main.go` |
| Build | Multi-stage Docker, `CGO_ENABLED=0`, statically linked binary |
| Module path | `github.com/hskvinh/backend` |

---

## 2. Project Structure

```
cmd/
└── api/
    └── main.go             # Entry point: wires config, DB, services, handlers, routes, HTTP server

internal/
├── audit/                  # Cross-cutting audit log domain
├── auth/                   # Account management, JWT issuance
├── candidate/              # Candidate profile and documents
├── certificate/            # Certificate issuance and delivery
├── config/                 # Configuration loading (config.go)
├── database/               # PostgreSQL connection pool (postgres.go)
├── exam/                   # Exam types, rooms, sessions, seat generation
├── location/               # Provinces and wards (public reference data)
├── middleware/              # HTTP middleware: auth.go, cors.go, logger.go, ratelimit.go
├── notification/           # Notifications
├── payment/                # Payment creation, callback handling
├── registration/           # Exam registration, seat reservation
├── result/                 # Exam results, recheck requests
└── utils/                  # Shared HTTP helpers, validation, password

migrations/
├── 001_schema.sql          # Full DB schema (applied first)
└── 002_seed_data.sql       # Seed / reference data (applied second)

Dockerfile                  # Multi-stage build
docker-compose.yml          # PostgreSQL 16, Go API, Adminer
.env.example                # Environment variable template
```

### Per-domain module layout

Every domain under `internal/<domain>/` follows this exact layout:

```
internal/<domain>/
├── dto/
│   ├── request/            # Request DTOs (one file per concern)
│   └── response/           # Response DTOs (one file per concern)
├── enum/                   # Domain-scoped typed string enums
├── handler.go              # HTTP handlers — thin, no business logic
├── mapper.go               # Entity → response DTO mapping functions
├── model.go                # *Entity structs that mirror DB tables 1:1
├── route.go                # Route registration via RegisterRoutes(...)
└── service.go              # Business logic + raw SQL queries
```

All new features must follow this layout exactly.

---

## 3. Architecture Layers

```
HTTP Request
    ↓
Global Middleware (CORS → Logger → Recovery → RateLimit)
    ↓
Route-level Middleware (JWTAuth → RequireRole if protected)
    ↓
Handler (gin.Context → parse → validate → call service → respond)
    ↓
Service (business rules, validation, DB queries, audit logging)
    ↓
database/sql (parameterized queries, transactions)
    ↓
PostgreSQL 16
```

**Layer responsibilities:**

- **Handler** — bind JSON/params, call `utils.Validate.Struct()`, call service, call `utils.*` response helper, return. Nothing else.
- **Service** — owns all business logic, SQL queries, transaction management, audit logging. Receives and returns domain types and DTOs.
- **Model** (`*Entity`) — plain structs mirroring DB columns 1:1. No methods beyond what the domain absolutely needs.
- **Mapper** — pure functions converting `*Entity → ResponseDTO`. No business logic.
- **DTO** — transport layer only. Request DTOs carry `validate` struct tags. Response DTOs carry `json` tags.
- **Route** — registers URL paths, HTTP methods, and attaches middleware. No logic.
- **Middleware** — cross-cutting HTTP concerns only.

**Violations to avoid:**
- Do not put SQL inside handlers.
- Do not put HTTP logic (status codes, `gin.Context`) inside services.
- Do not bypass middleware for protected routes.
- Do not put mapping logic inside handlers or services; use `mapper.go`.
- Do not expose raw `*Entity` structs as API responses; always map to a response DTO.

---

## 4. Dependency Injection

Dependencies are wired manually in `cmd/api/main.go`.

Constructor pattern:

```go
svc := domain.NewService(db, auditSvc)
h   := domain.NewHandler(svc)
domain.RegisterRoutes(api, h, cfg.JWTSecret)
```

`auditSvc` is wired once and injected into every service that requires it.

`payment.Service` receives `*registration.Service` as a constructor argument because payment confirmation must trigger registration confirmation.

Do **not** introduce a dependency injection framework. Do **not** use `init()` or global singletons. Configuration and `*sql.DB` are passed explicitly.

---

## 5. Configuration

Configuration lives in `internal/config/config.go`. The `Config` struct holds all values.

- `godotenv.Load()` reads `.env` (optional; missing is non-fatal).
- `mustGetEnv` calls `log.Fatalf` if a required variable is absent. Required variables: `DATABASE_URL`, `JWT_SECRET`.
- All other variables have defaults.
- Configuration is validated at startup (parsing durations, integers).
- `cfg` is passed into constructors. Do not read `os.Getenv` anywhere outside `config.go`.

**Add new configuration in this order:**
1. Add a field to `Config`.
2. Load it in `Load()` with `getEnv` or `mustGetEnv`.
3. Add the key to `.env.example` with a safe default.
4. Inject `cfg` (or the specific field) into the constructor that needs it.

Never hard-code secrets, ports, or connection strings.

---

## 6. Database Access

- `*sql.DB` is passed directly into service constructors. There is no repository layer; services own their SQL.
- All queries use `$N` positional placeholders (PostgreSQL style).
- Always call `rows.Close()` with `defer rows.Close()` immediately after `QueryContext`.
- Always check `rows.Err()` after the scan loop.
- All `RETURNING` clauses scan into the entity immediately.

### Transactions

Pattern used consistently across all services:

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

`defer tx.Rollback()` is always present. Rollback after a successful commit is a no-op in `database/sql`.

Audit logging inside a transaction is done by passing `tx` to `auditSvc.Log(ctx, tx, ...)`. For standalone (non-transactional) audit events, pass `nil`.

### Seat reservation and concurrency

`registration.Service.ReserveSeat` uses `SELECT ... FOR UPDATE SKIP LOCKED` to safely acquire the first available seat without races. This is the established pattern for any operation that must atomically claim a shared resource.

For any new feature touching `exam_seats`, `exam_registrations`, or `payments` that can be triggered by concurrent requests, use `SELECT ... FOR UPDATE` to lock the row before modifying state.

### Schema conventions (derived from `001_schema.sql`)

- Primary keys: `BIGINT GENERATED ALWAYS AS IDENTITY`
- All tables implement soft-delete: `*_is_deleted BOOLEAN DEFAULT FALSE`, `*_deleted_at TIMESTAMPTZ`
- All tables have `*_created_at TIMESTAMPTZ NOT NULL DEFAULT now()` and `*_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at` is maintained by a shared `fn_touch_updated_at()` trigger applied to every table
- All column names are prefixed with the table name (e.g., `account_username`, `exam_seat_status`)
- All queries must include `AND <table>_is_deleted = FALSE` when soft-delete is applicable
- Migrations are plain `.sql` files applied in numeric order

**Never modify the schema directly.** Add a new numbered `.sql` migration file.

---

## 7. HTTP API Conventions

### Base path

All API routes are mounted under `/api/v1`.

### Route naming (established patterns)

| Domain | Routes |
|---|---|
| auth | `POST /auth/register`, `POST /auth/login` |
| auth (self) | `GET /me`, `PUT /me`, `DELETE /me` |
| candidate | `GET/POST /candidates`, `GET/PUT/DELETE /candidates/:id` |
| exam | `GET /exam-types`, `GET /exam-rooms`, `GET /exam-sessions`, `GET /exam-sessions/:id`, `GET /exam-sessions/:id/seats` |
| exam (admin) | `POST /admin/exam-sessions/:id/seats/generate` |
| registration | `POST /registrations`, `GET /registrations`, `PUT /registrations/:id/confirm`, `DELETE /registrations/:id` |
| payment | `POST /payments`, `POST /payments/callback`, `GET /payments` |
| result | `POST /results`, `GET /results/:registration_id`, `POST /results/:id/recheck`, `PUT /admin/results/recheck/:id` |
| certificate | `POST /certificates`, `GET /certificates/:registration_id`, `PUT /certificates/:id/delivery` |
| notification | `GET /notifications` |
| location | `GET /provinces`, `GET /provinces/:id/wards` |
| audit (admin) | `GET /admin/audit-logs` |

### Response envelope

Every endpoint returns the same JSON envelope:

```json
{
  "success": true,
  "data":    <payload>,
  "error":   null,
  "meta":    null
}
```

Use the helpers in `internal/utils/response.go`:

```go
utils.OK(c, data)                   // 200
utils.OKWithMeta(c, data, &meta)    // 200 + pagination meta
utils.Created(c, data)              // 201
utils.BadRequest(c, msg)            // 400
utils.Unauthorized(c, msg)          // 401
utils.Forbidden(c, msg)             // 403
utils.NotFound(c, msg)              // 404
utils.Conflict(c, msg)              // 409
utils.TooManyRequests(c, msg)       // 429
utils.InternalServerError(c, msg)   // 500
```

Never call `c.JSON` directly in handlers. Use only these helpers.

### Path parameters

Parse path params in the handler using `strconv.ParseInt` or the local `paramID` helper. Return `utils.BadRequest` immediately if parsing fails.

### Validation

```go
var req request.SomeRequestDTO
if err := c.ShouldBindJSON(&req); err != nil {
    utils.BadRequest(c, "invalid request body")
    return
}
if err := utils.Validate.Struct(req); err != nil {
    utils.BadRequest(c, err.Error())
    return
}
```

Always bind first, then validate. `utils.Validate` is a package-level `*validator.Validate` instance.

Request DTOs use standard `validate` struct tags: `required`, `min`, `max`, `email`, `omitempty`.

---

## 8. Authentication and Authorization

### Mechanism

JWT, HMAC-SHA256 (`jwt.SigningMethodHS256`). Access token TTL: 24 h. Refresh token TTL: 168 h (7 days).

JWT payload contains `sub` (account ID as `int64`), `exp`, `iat`.

The middleware (`internal/middleware/auth.go`) extracts `user_id` and `user_role` into `gin.Context`:

```go
middleware.ContextKeyUserID   // "user_id"  → int64
middleware.ContextKeyUserRole // "user_role" → string
```

### Protecting routes

```go
// JWT only
g.Use(middleware.JWTAuth(jwtSecret))

// JWT + role enforcement
g.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
```

Both middlewares are applied at the group level in `route.go`. Public routes receive no middleware.

### Extracting the authenticated user in handlers

```go
func mustAccountID(c *gin.Context) int64 {
    v, _ := c.Get(middleware.ContextKeyUserID)
    return v.(int64)
}
```

Every domain handler file that needs the caller's ID must define this helper locally (it is not shared; each domain has its own copy — follow this pattern).

### Roles

Known roles: `admin`, `candidate` (and any others seeded in `002_seed_data.sql`).

Use `middleware.RequireRole("admin")` for admin-only endpoints. Never trust role values supplied by the client.

### Security rules

- Never log tokens, passwords, or the JWT secret.
- Never expose bcrypt hashes or internal error details to clients.
- Login failures always return generic "invalid credentials" — never distinguish between "user not found" and "wrong password."
- Account status `locked` prevents login.

---

## 9. Audit Logging

`audit.Service` is an interface:

```go
type Service interface {
    Log(ctx context.Context, tx *sql.Tx, req request.CreateAuditLogRequestDTO) error
    List(ctx context.Context, req request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error)
}
```

This is **the only domain that exposes an interface** — specifically to allow injection into other services and to facilitate test doubles.

Every state-changing operation (create, update, delete, payment, login, etc.) must call `auditSvc.Log(...)` **inside the same transaction** if one is open, or with `tx = nil` for standalone events.

Audit calls are intentionally non-fatal: callers use `_ = s.auditSvc.Log(...)` when audit failure should not abort the primary operation.

Valid `AuditAction` values: `CREATE`, `UPDATE`, `DELETE`, `SOFT_DELETE`, `RESTORE`, `LOGIN`, `LOGOUT`, `PASSWORD_CHANGE`, `PAYMENT`, `REFUND`, `EXPORT`, `APPROVE`, `REJECT`, `RECHECK`.

Valid `AuditEntity` values: `accounts`, `candidates`, `candidate_documents`, `exam_rooms`, `exam_sessions`, `exam_seats`, `exam_registrations`, `payments`, `exam_results`, `exam_recheck_requests`, `certificates`, `certificate_deliveries`.

Do not use raw strings. Use the typed constants from `internal/audit/enum`.

---

## 10. Domain Enums

Each domain defines its own typed string enum in `<domain>/enum/`:

```go
type SomeStatus string

const (
    StatusFoo SomeStatus = "foo"
    StatusBar SomeStatus = "bar"
)

func (s SomeStatus) Valid() bool { ... }
func (s SomeStatus) String() string { return string(s) }
```

Every enum type provides `Valid()` and `String()`. Validate enum values at the service boundary using `.Valid()` before persisting. Never store arbitrary client-supplied strings as enum values.

Known enums:

| Domain | Enum type | Values |
|---|---|---|
| auth | `AccountStatus` | `active`, `locked` |
| registration | `ExamRegistrationStatus` | `pending_payment`, `confirmed`, `cancelled` |
| payment | `PaymentMethod` | `vnpay`, `momo`, `zalopay`, `bank_transfer`, `cash` |
| payment | `PaymentStatus` | `pending`, `success`, `failed`, `refunded` |
| exam | `ExamSeatStatus` | `available`, `held`, `booked` |

Inspect the per-domain `enum/` directory for other enum types in `exam`, `result`, and `certificate`.

---

## 11. Models

Entity structs live in `<domain>/model.go`. They mirror database tables 1:1:

- Named `<TableName>Entity` (e.g., `AccountEntity`, `ExamSeatEntity`).
- All fields prefixed with the table name in PascalCase (e.g., `AccountEmail`, `ExamSeatStatus`).
- Nullable columns use pointer types (`*string`, `*time.Time`).
- Soft-delete fields (`IsDeleted bool`, `DeletedAt *time.Time`) are included on every entity.

Do not add methods to entity structs. Do not use entity structs as API response types.

---

## 12. DTOs

**Request DTOs** (`dto/request/`):
- Named `<Action><Domain>RequestDTO` (e.g., `CreateAccountRequestDTO`, `ReserveSeatRequestDTO`).
- Carry `json` and `validate` struct tags.
- No business logic.

**Response DTOs** (`dto/response/`):
- Named `<Domain>ResponseDTO` (e.g., `AccountResponseDTO`, `PaymentResponseDTO`).
- Carry `json` struct tags.
- No business logic.

Do not reuse one DTO for unrelated operations. Do not expose entity structs as API responses.

---

## 13. Mappers

Every domain has a `mapper.go` containing pure functions:

```go
func ToSomeResponseDTO(e SomeEntity) response.SomeResponseDTO {
    return response.SomeResponseDTO{
        ID: e.SomeID,
        // ...
    }
}
```

- Mapping logic lives **only** in `mapper.go`.
- Handlers and services call the mapper; they do not inline field assignments.
- Mappers are deterministic and side-effect-free.

---

## 14. Middleware

| Middleware | File | Behaviour |
|---|---|---|
| `CORS()` | `cors.go` | Permissive CORS headers; short-circuits `OPTIONS`. |
| `Logger()` | `logger.go` | Custom timestamp/IP/method/path/status/latency format. |
| `gin.Recovery()` | Gin built-in | Recovers panics, returns 500. |
| `limiter.RateLimit()` | `ratelimit.go` | Per-IP token bucket (RPS + burst). Responds 429. Cleanup goroutine runs every 5 min. |
| `JWTAuth(secret)` | `auth.go` | Validates Bearer token; sets `user_id` and `user_role` on context. |
| `RequireRole(roles...)` | `auth.go` | Enforces role; must follow `JWTAuth`. |

Global middleware order (applied in `main.go`): `CORS → Logger → Recovery → RateLimit`.

Do not reorder global middleware without understanding the security implications.

---

## 15. Error Handling

### In services

Wrap errors with `fmt.Errorf("domain: operation: %w", err)`.

Use `errors.Is(err, sql.ErrNoRows)` to detect not-found, then return a plain `errors.New("entity not found")` for the handler to surface.

```go
if errors.Is(err, sql.ErrNoRows) {
    return ..., errors.New("account not found")
} else if err != nil {
    return ..., fmt.Errorf("auth: get account: %w", err)
}
```

### In handlers

Map service errors to the appropriate `utils.*` response helper:

```go
if err != nil {
    utils.NotFound(c, err.Error())   // or Conflict, BadRequest, etc.
    return
}
```

Do not call `utils.InternalServerError` with raw database error messages. Use a generic message: `"failed to fetch exam types"`.

Never panic in request handling.

### Error classification

| Condition | HTTP helper |
|---|---|
| JSON binding failure | `BadRequest` |
| Validation failure | `BadRequest` |
| Not found (ErrNoRows) | `NotFound` |
| Business rule violation | `Conflict` |
| Invalid credentials | `Unauthorized` |
| Insufficient role | `Forbidden` |
| Database/infrastructure error | `InternalServerError` (generic msg) |

---

## 16. Context Propagation

- Every DB call receives `ctx` from `c.Request.Context()`.
- Services accept `ctx context.Context` as first parameter.
- Pass `ctx` to every `QueryContext`, `QueryRowContext`, `ExecContext`, `BeginTx` call.
- Do not create `context.Background()` inside request handlers or services.
- Do not store context in structs.

---

## 17. Concurrency

### Seat reservation

The only safe way to reserve a seat is `SELECT ... FOR UPDATE SKIP LOCKED`:

```sql
SELECT exam_seat_id FROM exam_seats
WHERE exam_seat_exam_session_id = $1
  AND exam_seat_status = 'available'
  AND exam_seat_is_deleted = FALSE
ORDER BY exam_seat_number ASC LIMIT 1
FOR UPDATE SKIP LOCKED
```

This is done inside a transaction. Do not implement seat reservation with application-level checks only.

### Payment callbacks

`ProcessCallback` locks the payment row with `SELECT ... FOR UPDATE` before updating status. This prevents duplicate callback processing. Only `pending` payments can transition.

### Held seat expiry

`AutoReleaseHeldSeats` releases held seats whose `exam_seat_held_until < NOW()` in a single atomic `UPDATE`. Seats are held for 15 minutes after reservation.

### Rate limiter goroutine

The `RateLimiter` cleanup goroutine is started in `NewRateLimiter` and runs for the lifetime of the process. This is intentional. It removes stale per-IP entries every 5 minutes.

### Rules for new goroutines

Introduce a goroutine only when it provides concrete value. For every goroutine:

1. Define its owner, lifetime, and stop condition.
2. Handle errors explicitly.
3. Do not use a request `context.Context` in a goroutine that outlives the request.

---

## 18. Payment State Machine

Valid payment status transitions:

```
pending → success
pending → failed
pending → refunded
```

No other transitions are permitted. The service enforces `pay.PaymentStatus != payenum.PayStatusPending` before accepting a callback.

When a payment transitions to `success`, `payment.Service.ProcessCallback` calls `registration.Service.ConfirmRegistration`, which moves the registration to `confirmed` and the seat to `booked`.

Do not allow arbitrary client-supplied status transitions.

---

## 19. Registration State Machine

```
(created) → pending_payment → confirmed
                            → cancelled
pending_payment             → cancelled
```

A `cancelled` registration cannot be confirmed. Cancellation releases the held seat back to `available`.

---

## 20. Logging

The project uses the Go standard library `log` package only:

```go
log.Printf("[domain] message: %v", err)
log.Println("[main] listening on :8080")
log.Fatalf("[main] fatal: %v", err)
```

- Prefix log messages with `[package]` for traceability.
- Use `log.Fatalf` only at startup for unrecoverable configuration or DB failures.
- Do not log passwords, JWT tokens, or sensitive payment information.
- Do not introduce a third-party structured logger unless the project explicitly adopts one.
- The HTTP middleware (`logger.go`) logs each request's timestamp, IP, method, path, status, and latency automatically.

---

## 21. Validation

Two validation mechanisms exist — use the correct one for the context:

**Struct-tag validation** (preferred for request DTOs):
```go
utils.Validate.Struct(req)   // uses go-playground/validator/v10
```

**Imperative validation** (`utils.Validator`, for complex or conditional logic):
```go
v := utils.NewValidator()
v.Required("field", value).MinLen("field", value, 3)
if v.HasErrors() {
    utils.BadRequest(c, v.FirstError())
    return
}
```

Enum validation happens inside the service using `enum.Valid()`.

Do not duplicate validation between the handler and the service for the same input.

---

## 22. Security

- Never hard-code secrets. Use environment variables.
- Never commit a real `.env` file. `.env.example` only.
- `mustGetEnv` calls `log.Fatalf` at startup if `DATABASE_URL` or `JWT_SECRET` are absent.
- Passwords are hashed with bcrypt at `bcrypt.DefaultCost`. Never store or log plain-text passwords.
- SQL injection: all queries use positional `$N` parameters. String interpolation into SQL is forbidden.
- Soft-delete: always filter `AND *_is_deleted = FALSE` in queries — omitting this leaks deleted records.
- CORS: currently permissive (`*`). In production, restrict `AllowOrigins` to the frontend domain.
- Rate limiting: global per-IP token bucket at 10 RPS / 20 burst. Do not remove without a replacement.

---

## 23. Adding a New Domain

Follow this checklist:

1. Create `internal/<domain>/` directory.
2. Create `model.go` with `*Entity` structs mirroring the DB tables.
3. Create `dto/request/<domain>_request.go` and `dto/response/<domain>_response.go`.
4. Create `enum/<domain>_enum.go` if the domain has typed string states.
5. Create `service.go` with `Service` struct, `NewService(db *sql.DB, auditSvc audit.Service, ...) *Service`, and business methods.
6. Create `mapper.go` with `To<Domain>ResponseDTO(e Entity) ResponseDTO` functions.
7. Create `handler.go` with `Handler` struct, `NewHandler(svc *Service) *Handler`, and HTTP method functions.
8. Create `route.go` with `RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string)` applying appropriate middleware.
9. Wire in `cmd/api/main.go`: service → handler → RegisterRoutes.
10. Add migration SQL if new tables are required.

---

## 24. Adding a New Endpoint to an Existing Domain

1. Add the request/response DTO if new fields are required.
2. Add the business method to `service.go`.
3. Add the mapper function to `mapper.go` if a new entity is returned.
4. Add the handler method to `handler.go`.
5. Register the route in `route.go` with the correct HTTP method and middleware.
6. Audit-log the action inside the service transaction.

---

## 25. Database Migrations

Migrations are plain SQL files under `migrations/`. Applied at container startup by PostgreSQL `docker-entrypoint-initdb.d` (alphabetical order).

- `001_schema.sql` — full schema, all tables, indexes, triggers.
- `002_seed_data.sql` — roles, exam types, provinces, wards, other reference data.

To add a schema change:
1. Create `migrations/003_<description>.sql`.
2. Write idempotent SQL (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `ALTER TABLE IF NOT EXISTS`).
3. Do **not** modify `001_schema.sql` or `002_seed_data.sql` once they have been applied to production.

Reset local DB:
```bash
docker compose down -v && docker compose up -d
```

---

## 26. Verification Commands

Run before considering any change complete:

```bash
gofmt -l ./...
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

The project currently has no test files. When writing tests, place them in the same package (`package <domain>`) using the standard `testing` package. Prefer table-driven tests.

---

## 27. Docker

```bash
# Start all services
docker compose up -d

# Rebuild API after code changes
docker compose up -d --build api

# View API logs
docker compose logs -f api

# Reset database (removes all data)
docker compose down -v
```

Build command: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api`

Runtime: `alpine:latest` as non-root `appuser`.

---

## 28. Feature Implementation Workflow

For any non-trivial change:

1. Read the affected domain's `service.go`, `model.go`, `handler.go`, `route.go`.
2. Identify the business rules and state transitions involved.
3. Identify any concurrent-access risks (registration, payment, seat capacity).
4. Implement the smallest correct change.
5. Add audit logging for state-changing operations.
6. Run `go vet ./...` and `go build ./...`.
7. Run `go test -race ./...` when tests are present.

Do not start writing code for operations touching `registration`, `payment`, `exam_seats`, or `exam_results` without first tracing the transaction boundaries and understanding the state machine.

---

## 29. What Does NOT Exist in This Project

Do not invent or introduce the following unless explicitly required by a task:

- Redis or any caching layer
- Repository interfaces (there is no repository pattern; services own SQL)
- Event bus or message queue
- gRPC
- Structured/JSON logging library (zerolog, zap, slog)
- Dependency injection container
- `common/`, `helpers/`, `base/`, `managers/` packages
- A separate domain model distinct from the entity
- Clean Architecture layering beyond what is documented here
- Any ORM or query builder

The existing code is intentionally simple. Preserve that simplicity.

---

## 30. Code Review Priorities

When reviewing any change, check in this order:

1. **Correctness** — does it do the right thing?
2. **Security** — auth, authz, SQL injection, credential exposure?
3. **Data consistency** — transactions, state machines, concurrent access?
4. **Error handling** — are errors handled, wrapped, mapped correctly?
5. **Audit logging** — is the action logged inside the transaction?
6. **Response envelope** — does the handler use `utils.*` helpers?
7. **Soft-delete** — are queries filtering `_is_deleted = FALSE`?
8. **Enum validation** — are enum values validated before persistence?
9. **Architecture boundaries** — no business logic in handlers, no HTTP in services?
10. **Tests** — are observable behaviors verified?
