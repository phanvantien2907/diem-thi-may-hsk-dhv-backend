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
| ORM / query builder | **None** — raw SQL with `database/sql` |
| Authentication | JWT (`github.com/golang-jwt/jwt/v5 v5.2.1`), HMAC-SHA256, Bearer token |
| Password hashing | bcrypt (`golang.org/x/crypto`) |
| Validation | `github.com/go-playground/validator/v10 v10.22.0` via `utils.Validate.Struct()` |
| Configuration | `github.com/joho/godotenv v1.5.1` + `os.LookupEnv` |
| Logging | Standard library `log` package only |
| Migration tool | `golang-migrate/migrate` via Docker compose |
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
├── audit/                  # Cross-cutting audit log domain (only domain with an exported interface)
├── auth/                   # Account management, JWT issuance, session management
├── candidate/              # Candidate profile and identity documents (with Cloudinary upload)
├── certificate/            # Certificate issuance and delivery
├── config/                 # Configuration loading (config.go)
├── database/               # PostgreSQL connection pool (postgres.go)
├── exam/                   # Exam types, rooms, sessions, seat generation
├── location/               # Provinces and wards (public reference data)
├── media/                  # Cloudinary file upload/delete service (shared infrastructure)
├── middleware/              # auth.go, cors.go, logger.go, ratelimit.go
├── notification/           # System notifications
├── payment/                # Payment creation and gateway callback handling
├── registration/           # Exam registration and seat reservation
├── result/                 # Exam results and recheck requests
└── utils/                  # Shared HTTP response helpers, validation, password hashing, param parsing

migrations/
├── 000001_schema.up.sql          # Full DB schema (applied first)
├── 000001_schema.down.sql        # Rollback for schema
├── 000002_seed_data.up.sql       # Seed / reference data
└── 000002_seed_data.down.sql     # Rollback for seed data

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
├── entities/               # *Entity structs mirroring DB tables 1:1 (one file per table)
├── enum/                   # Domain-scoped typed string enums
├── handler.go              # HTTP handlers — thin, no business logic
├── mapper.go               # Entity → response DTO mapping functions
├── route.go                # Route registration via RegisterRoutes(...)
└── service.go              # Business logic + raw SQL queries
```

> **Note:** The subdirectory is `entities/` (not a flat `model.go`). Entity structs live in separate files within that package (e.g., `account_entities.go`, `session_entities.go`). Some domains also define input structs in `entities/` (e.g., `CreateProfileInput`, `UploadDocumentInput`) when the service needs a normalized form of the request DTO.

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

- **Handler** — bind JSON/params/query, call `utils.Validate.Struct()`, call service, call `utils.*` response helper, return. Nothing else.
- **Service** — owns all business logic, SQL queries, transaction management, audit logging. Receives and returns domain types and DTOs.
- **Entity** (`*Entity`) — plain structs mirroring DB columns 1:1, living in the `entities/` subdirectory. Some domains also define internal input structs (e.g., `CreateProfileInput`, `UploadDocumentInput`) in the same `entities/` package when the service needs a normalized form of the request DTO.
- **Mapper** — pure functions converting `*Entity → ResponseDTO`. No business logic.
- **DTO** — transport layer only. Request DTOs carry `validate` struct tags. Response DTOs carry `json` tags.
- **Route** — registers URL paths, HTTP methods, attaches middleware. No logic.
- **Middleware** — cross-cutting HTTP concerns only.

**Violations to avoid:**
- Do not put SQL inside handlers.
- Do not put HTTP logic (status codes, `gin.Context`) inside services.
- Do not bypass middleware for protected routes.
- Do not put mapping logic inside handlers or services; use `mapper.go`.
- Do not expose raw `*Entity` structs as API responses.

---

## 4. Dependency Injection

Dependencies are wired manually in `cmd/api/main.go`.

Constructor pattern:

```go
svc := domain.NewService(db, auditSvc)
h   := domain.NewHandler(svc)
domain.RegisterRoutes(api, h, cfg.JWTSecret)
```

`auditSvc` is instantiated once and injected into every service that requires it.

`payment.Service` receives `*registration.Service` as a constructor argument because a successful payment must trigger registration confirmation.

`notification.Service` and `location.Service` receive only `*sql.DB` — they have no audit dependency.

Do **not** introduce a dependency injection framework. Do **not** use `init()` or global singletons.

---

## 5. Configuration

Configuration lives in `internal/config/config.go`. The `Config` struct holds all values.

- `godotenv.Load()` reads `.env` (optional; missing is non-fatal in production).
- `mustGetEnv` calls `log.Fatalf` if a required variable is absent.
- Required variables: `DATABASE_URL`, `JWT_SECRET`.
- All other variables have safe defaults.
- Configuration is validated at startup (parsing durations, integers fail fast).
- `cfg` is passed into constructors. Do **not** read `os.Getenv` anywhere outside `config.go`.

**Add new configuration in this order:**
1. Add a field to `Config`.
2. Load it in `Load()` with `getEnv` or `mustGetEnv`.
3. Add the key to `.env.example` with a safe default.
4. Inject the specific field into the constructor that needs it.

Never hard-code secrets, ports, or connection strings.

---

## 6. Database Access

- `*sql.DB` is passed directly into service constructors. There is no repository layer; services own their SQL.
- All queries use `$N` positional placeholders (PostgreSQL style).
- Always `defer rows.Close()` immediately after a successful `QueryContext`.
- Always check `rows.Err()` after a scan loop.
- All `RETURNING` clauses scan into the entity struct immediately.

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

Audit logging inside a transaction passes `tx` to `auditSvc.Log(ctx, tx, ...)`. For standalone events, pass `nil`.

### Seat reservation and concurrency

`registration.Service.ReserveSeat` uses `SELECT ... FOR UPDATE SKIP LOCKED` to atomically acquire the first available seat without races. This is the established pattern for claiming a shared resource.

For any new feature touching `exam_seats`, `exam_registrations`, or `payments` that can be called concurrently, lock the row with `SELECT ... FOR UPDATE` before modifying state.

### Schema conventions (from `001_schema.sql`)

- Primary keys: `BIGINT GENERATED ALWAYS AS IDENTITY`
- All tables implement soft-delete: `*_is_deleted BOOLEAN DEFAULT FALSE`, `*_deleted_at TIMESTAMPTZ`
- All tables have `*_created_at TIMESTAMPTZ NOT NULL DEFAULT now()` and `*_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at` is maintained by the shared `fn_touch_updated_at()` trigger
- All column names are prefixed with the table name (e.g., `account_username`, `exam_seat_status`)
- **All queries must include `AND <table>_is_deleted = FALSE`** when soft-delete applies — omitting this leaks deleted records
- Migrations are managed by `golang-migrate` using `*.<up/down>.sql` files

**Never modify the schema directly.** Add a new numbered `.sql` migration file.

---

## 7. HTTP API Conventions

### Base path

All API routes are mounted under `/api/v1`.

### Actual routes (verified against route.go files)

| Domain | Method | Path | Auth |
|---|---|---|---|
| auth | POST | `/auth/register` | public |
| auth | POST | `/auth/login` | public |
| auth | POST | `/auth/refresh` | public |
| auth | POST | `/auth/force-password-change` | public |
| auth | POST | `/auth/logout` | JWT |
| auth | POST | `/auth/logout-all` | JWT |
| auth | GET | `/me` | JWT |
| auth | PUT | `/me` | JWT |
| auth | DELETE | `/me` | JWT |
| auth | PATCH | `/me/password` | JWT |
| auth | GET | `/me/sessions` | JWT |
| auth | DELETE | `/me/sessions/:id` | JWT |
| auth | DELETE | `/admin/delete-account/:id` | JWT + admin |
| auth | POST | `/admin/staff-accounts` | JWT + admin |
| candidate | POST | `/candidates` | JWT |
| candidate | PUT | `/candidates/:id` | JWT |
| candidate | POST | `/candidates/:id/documents` | JWT |
| candidate | GET | `/candidates/:id/documents` | JWT |
| exam | GET | `/exam-types` | public |
| exam | GET | `/exam-rooms` | public |
| exam | GET | `/exam-sessions` | public |
| exam | GET | `/exam-sessions/:id` | public |
| exam | GET | `/exam-sessions/:id/seats` | public |
| exam | POST | `/admin/exam-sessions/:id/seats/generate` | JWT + admin |
| registration | POST | `/registrations` | JWT |
| registration | GET | `/registrations?candidate_id=X` | JWT |
| registration | PUT | `/registrations/:id/confirm` | JWT |
| registration | DELETE | `/registrations/:id` | JWT |
| payment | POST | `/payments` | JWT |
| payment | POST | `/payments/callback` | JWT |
| payment | GET | `/payments?candidate_id=X` | JWT |
| result | GET | `/results/:registration_id` | JWT |
| result | POST | `/rechecks` | JWT |
| result | POST | `/admin/results` | JWT + admin |
| certificate | GET | `/certificates/:result_id` | JWT |
| certificate | POST | `/certificates/:id/deliveries` | JWT |
| certificate | POST | `/admin/certificates` | JWT + admin |
| notification | GET | `/notifications` | JWT |
| notification | PUT | `/notifications/:id/read` | JWT |
| location | GET | `/locations/provinces` | public |
| location | GET | `/locations/provinces/:id/wards` | public |
| location | GET | `/locations/wards/:id/delivery-coverage` | public |
| location | GET | `/admin/locations/stats/candidates-by-province` | JWT + admin |
| location | POST | `/admin/locations/wards/:id/merge` | JWT + admin |
| audit | GET | `/admin/audit-logs` | JWT + admin |

### Response envelope

**Success Responses (HTTP 2xx):**
Every successful endpoint returns the standard JSON envelope:

```json
{
  "success": true,
  "data":    <payload>,
  "meta":    null
}
```

**Exception Responses (HTTP 4xx, 5xx):**
All error responses use the `internal/exception` module and return the RESTful error envelope:

```json
{
  "statusCode": 400,
  "timestamp": "2026-09-03T16:17:02+07:00",
  "path": "/api/v1/...",
  "msg": "Thông báo lỗi chi tiết"
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

**Never call `c.JSON` directly in handlers.** Use only these helpers.

Middleware-level error responses (e.g., 401 from `JWTAuth`, 429 from `RateLimit`) call `c.AbortWithStatusJSON` directly — this is acceptable for middleware only.

### Path and query parameters

Path params: parsed via the shared helper `utils.ParamID(c, "key") (int64, bool)` in `internal/utils/param.go`. Do **not** define per-domain local `paramID` functions.

Query params: bound with `c.ShouldBindQuery(&q)`.

Return `utils.BadRequest` immediately if a required param is missing or invalid.

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

Always bind first, then validate. `utils.Validate` is a package-level `*validator.Validate` singleton.

---

## 8. Authentication and Authorization

### JWT mechanism

JWT, HMAC-SHA256 (`jwt.SigningMethodHS256`). Access token TTL: 24 h. Refresh token TTL: 168 h.

**Token claims:** `auth.Service.issueAccessToken` embeds `sub` (account ID), `user_id`, `user_role`, `exp`, and `iat`. The role is fetched via a JOIN on the `roles` table at login/refresh time. `RequireRole` middleware reads `user_role` from the token claims.

### Protecting routes

```go
// JWT only
g.Use(middleware.JWTAuth(jwtSecret))

// JWT + role enforcement
g.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
```

Applied at the group level in `route.go`. Public routes receive no middleware.

### Extracting the authenticated user in handlers

Use the shared helper in `internal/utils/account_id.go`:

```go
accountID := utils.MustAccountID(c)  // panics if not authenticated — only use behind JWTAuth middleware
```

Do **not** define per-domain local `mustAccountID` / `accountID` helpers. The shared `utils.MustAccountID` is the established pattern across all domains.

### Roles

Known roles seeded in `002_seed_data.sql`: `admin`, `candidate`.

### Security rules

- Never log tokens, passwords, or the JWT secret.
- Never expose bcrypt hashes or internal error details to clients.
- Login failures always return generic "thông tin đăng nhập không chính xác" — do not distinguish between "user not found" and "wrong password."
- Account status `locked` prevents normal access. The `pending` status is no longer enforced at login (OTP verification was removed).
- **Authentication Identifiers:** Candidates login using CCCD (12 digits) or Email.
- **Passwords:** Must contain both letters and numbers, minimum 8 characters, at least one uppercase letter, and at least one special symbol (!, @, #, etc).

---

## 9. Audit Logging

`audit.Service` is an interface — **the only domain that exposes an exported interface**:

```go
type Service interface {
    Log(ctx context.Context, tx *sql.Tx, req request.CreateAuditLogRequestDTO) error
    List(ctx context.Context, req request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error)
}
```

This allows injection into other services and easy test-double substitution.

Every state-changing operation must call `auditSvc.Log(...)` **inside the same transaction** if one is open, or with `tx = nil` for standalone events.

Audit calls are intentionally non-fatal: callers use `_ = s.auditSvc.Log(...)`.

Valid `AuditAction` values (from `internal/audit/enum`):

```
CREATE, UPDATE, DELETE, SOFT_DELETE, RESTORE,
LOGIN, LOGOUT, PASSWORD_CHANGE,
PAYMENT, REFUND,
EXPORT, APPROVE, REJECT, RECHECK
```

Valid `AuditEntity` values (from `internal/audit/enum`):

```
accounts, candidates, candidate_documents,
exam_rooms, exam_sessions, exam_seats,
exam_registrations, payments,
exam_results, exam_recheck_requests,
certificates, certificate_deliveries
```

**Never use raw strings.** Use the typed constants from `internal/audit/enum`.

---

## 10. Domain Enums

Each domain defines its own typed string enum in `<domain>/enum/`. Every enum type provides `Valid()` and `String()`. Validate using `.Valid()` at the service boundary before persisting.

Complete enum reference:

| Domain | Type | Values |
|---|---|---|
| auth | `AccountStatus` | `active`, `locked`, `pending` |
| candidate | `CandidateGender` | `male`, `female`, `other` |
| candidate | `DocumentType` | `cccd`, `passport` |
| candidate | `DocumentVerificationStatus` | `pending`, `verified`, `rejected` |
| exam | `ExamShift` | `morning`, `afternoon`, `evening` |
| exam | `ExamSessionStatus` | `open`, `closed`, `cancelled` |
| exam | `ExamSeatStatus` | `available`, `held`, `booked` |
| registration | `ExamRegistrationStatus` | `pending_payment`, `confirmed`, `cancelled` |
| payment | `PaymentMethod` | `vnpay`, `momo`, `zalopay`, `bank_transfer`, `cash` |
| payment | `PaymentStatus` | `pending`, `success`, `failed`, `refunded` |
| result | `ExamResultStatus` | `pass`, `fail` |
| result | `RecheckStatus` | `submitted`, `in_review`, `completed`, `rejected` |
| certificate | `CertificateStatus` | `issued`, `revoked` |
| certificate | `DeliveryStatus` | `requested`, `shipped`, `delivered`, `failed` |
| notification | `NotificationChannel` | `email`, `sms`, `zalo`, `push` |
| notification | `NotificationStatus` | `pending`, `sent`, `failed` |

---

## 11. Entities

Entity structs live in `<domain>/entities/` as a Go package (one file per table or concern, e.g., `account_entities.go`, `province_entities.go`). 

Types of structs allowed in this package:
1. **DB-mirroring entities**: Mirror database tables 1:1. Named `<TableName>Entity`.
2. **Lean projections**: Subsets of table columns used for read-only APIs (e.g. `ProvinceEntity` without `deleted_at`).
3. **Aggregate entities**: Results of JOINs or GROUP BYs for specific queries (e.g. `ProvinceStatsEntity`).

Rules for DB-mirroring entities:

- Named `<TableName>Entity` (e.g., `AccountEntity`, `SessionEntity`, `CandidateDocumentEntity`).
- All fields prefixed with the table name in PascalCase (e.g., `AccountEmail`, `SessionDeviceInfo`).
- Nullable columns use pointer types (`*string`, `*time.Time`, `*int64`).
- Soft-delete fields (`IsDeleted bool`, `DeletedAt *time.Time`) are included on every entity.

Some domains also define **internal input structs** in the `entities/` package (e.g., `CreateProfileInput`, `UpdateProfileInput`, `UploadDocumentInput`). These carry validated, parsed data from request DTOs and are used as service method parameters when the service needs a form that differs from the raw DTO.

Do not add methods to entity structs. Do not expose entity structs as API responses.

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

Every domain has a `mapper.go` with pure mapping functions:

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
| `CORS()` | `cors.go` | Permissive CORS headers; short-circuits `OPTIONS` with 204. |
| `Logger()` | `logger.go` | Custom format: `timestamp \| IP \| METHOD path \| status \| latency`. |
| `gin.Recovery()` | Gin built-in | Recovers panics, returns 500. |
| `limiter.RateLimit()` | `ratelimit.go` | Per-IP token bucket. Responds 429. Background cleanup every 5 min. |
| `JWTAuth(secret)` | `auth.go` | Validates Bearer token; sets `user_id` (int64) and `user_role` (string) on Gin context. |
| `RequireRole(roles...)` | `auth.go` | Enforces role membership; must follow `JWTAuth`. |

Global middleware order (in `main.go`): `CORS → Logger → Recovery → RateLimit`.

Do not reorder global middleware without understanding the security and logging implications.

---

## 15. Error Handling

### Localization (Tiếng Việt)

- **All user-facing error messages must be in Vietnamese.** This includes any `errors.New("...")` returned from the service layer that is destined to bubble up to the client (e.g., via `utils.BadRequest(c, err.Error())`).
- Internal error wrapping (e.g., `fmt.Errorf("domain: operation: %w", err)`) and log messages should remain in English for developer traceability.

### In services

Wrap errors with `fmt.Errorf("domain: operation: %w", err)`.

Detect not-found with `errors.Is(err, sql.ErrNoRows)`, then return a plain `errors.New("Không tìm thấy thực thể")`:

```go
if errors.Is(err, sql.ErrNoRows) {
    return ..., errors.New("Không tìm thấy tài khoản")
} else if err != nil {
    return ..., fmt.Errorf("auth: get account: %w", err)
}
```

### In handlers

Map service errors to the appropriate `utils.*` helper. The actual mapping used per domain:

| Domain | Error condition | HTTP helper used |
|---|---|---|
| auth | business/not found | `utils.NotFound` or `utils.Conflict` |
| registration | conflicts (dup, no seats) | `utils.Conflict` |
| registration | infrastructure | `utils.InternalServerError` |
| payment | invalid method/status | `utils.BadRequest` |
| payment | callback conflicts | `utils.Conflict` |
| result | not found | `utils.NotFound` |
| result | create/recheck | `utils.InternalServerError` |
| certificate | not found | `utils.NotFound` |
| certificate | create | `utils.InternalServerError` |
| exam | list failures | `utils.InternalServerError` |
| notification | list failures | `utils.InternalServerError` |

Do not pass raw database error messages to `utils.InternalServerError`. Use a generic message like `"failed to fetch registrations"`.

Never panic in request handling.

---

## 16. Context Propagation

- Every DB call receives `ctx` from `c.Request.Context()`.
- Services accept `ctx context.Context` as first parameter.
- Pass `ctx` to every `QueryContext`, `QueryRowContext`, `ExecContext`, `BeginTx`.
- Do not create `context.Background()` inside request handlers or services.
- Do not store context in structs.

---

## 17. Concurrency

### Seat reservation

The only safe way to reserve a seat under concurrent load is `SELECT ... FOR UPDATE SKIP LOCKED`:

```sql
SELECT exam_seat_id FROM exam_seats
WHERE exam_seat_exam_session_id = $1
  AND exam_seat_status = 'available'
  AND exam_seat_is_deleted = FALSE
ORDER BY exam_seat_number ASC LIMIT 1
FOR UPDATE SKIP LOCKED
```

This is inside a transaction. Do not implement seat reservation with application-level checks only.

### Payment callbacks

`ProcessCallback` locks the payment row with `SELECT ... FOR UPDATE` before updating status, preventing duplicate processing. Only `pending` payments may transition.

### Held seat expiry

`AutoReleaseHeldSeats` reclaims seats with `exam_seat_held_until < NOW()` in a single atomic `UPDATE`. Seats are held for 15 minutes.

### In-memory rate limiter

`RateLimiter` is a `sync.Mutex`-protected token bucket keyed by client IP. It is process-local (not distributed). A background cleanup goroutine removes stale entries every 5 minutes — started in `NewRateLimiter`, runs for the process lifetime.

### Rules for new goroutines

Introduce a goroutine only when it provides concrete value. For every goroutine define:
1. Owner and lifetime.
2. Stop condition.
3. Error handling.
4. Whether it may outlive a request context (if yes, do not use `c.Request.Context()`).

---

## 18. State Machines

### Registration

```
(new) → pending_payment
pending_payment → confirmed   (via payment success callback)
pending_payment → cancelled
confirmed       → (terminal)
cancelled       → (terminal)
```

A cancelled registration cannot be confirmed. Cancellation frees the seat to `available`.

### Payment

```
pending → success   (triggers ConfirmRegistration)
pending → failed
pending → refunded
```

No other transitions. The service rejects any callback for a non-`pending` payment.

### Seat

```
available → held    (15-minute window, during reservation)
held      → booked  (on payment success / registration confirm)
held      → available (on cancellation or expiry)
booked    → available (on registration cancellation)
```

---

## 19. Logging

The project uses the standard library `log` package only:

```go
log.Printf("[domain] message: %v", err)
log.Println("[main] server started")
log.Fatalf("[main] fatal: %v", err)
```

- Prefix log messages with `[package]` for traceability.
- Use `log.Fatalf` only at startup for unrecoverable failures.
- Never log passwords, tokens, or sensitive payment data.
- Do not introduce a structured logging library unless the project explicitly adopts one.
- HTTP request logging is automatic via `middleware.Logger()`.

---

## 20. Validation

Two mechanisms exist — use the appropriate one:

**Struct-tag validation** (preferred for request DTOs):
```go
utils.Validate.Struct(req)   // go-playground/validator/v10
```

**Imperative validation** (`utils.Validator`, for complex/conditional logic):
```go
v := utils.NewValidator()
v.Required("field", value).MinLen("field", value, 3)
if v.HasErrors() {
    utils.BadRequest(c, v.FirstError())
    return
}
```

**Enum validation** at the service boundary:
```go
if !payenum.PaymentMethod(req.Method).Valid() {
    return ..., fmt.Errorf("invalid payment method: %s", req.Method)
}
```

Do not duplicate validation between the handler and the service for the same input.

---

## 21. Security

- Never hard-code secrets. Use environment variables.
- Never commit a real `.env` file. `.env.example` only.
- `mustGetEnv` calls `log.Fatalf` at startup if `DATABASE_URL` or `JWT_SECRET` are absent.
- Passwords hashed with bcrypt at `bcrypt.DefaultCost`. Never store or log plain text.
- All queries use positional `$N` parameters. String interpolation into SQL is **forbidden**.
- Always filter `AND *_is_deleted = FALSE` — omitting this leaks soft-deleted records.
- CORS: currently permissive (`*`). Restrict `AllowOrigins` in production.
- Rate limiting: global per-IP token bucket (10 RPS / 20 burst by default). Do not remove without a replacement.

---

## 22. Adding a New Domain

Checklist:

1. Create `internal/<domain>/` directory.
2. Create `entities/<table>_entities.go` with `*Entity` structs and optional input structs.
3. Create `dto/request/<domain>_request.go` and `dto/response/<domain>_response.go`.
4. Create `enum/<domain>_enum.go` if the domain has typed string states.
5. Create `service.go`: `Service` struct, `NewService(db *sql.DB, auditSvc audit.Service) *Service`, business methods.
6. Create `mapper.go`: `To<Domain>ResponseDTO(e Entity) ResponseDTO` functions.
7. Create `handler.go`: `Handler` struct, `NewHandler(svc *Service) *Handler`, HTTP method functions using `utils.MustAccountID(c)` for auth context.
8. Create `route.go`: `RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string)` with appropriate middleware.
9. Wire in `cmd/api/main.go`: service → handler → RegisterRoutes.
10. Add migration SQL if new tables are required.

If the new domain needs file uploads, inject `*media.Service` into the constructor (see `candidate.Service`).
If the new domain does not need audit logging (like `location` or `notification`), omit `auditSvc` from the constructor — follow the pattern of those domains.

---

## 23. Adding a New Endpoint to an Existing Domain

1. Add request/response DTOs if new fields are needed.
2. Add the business method to `service.go`.
3. Add the mapper function to `mapper.go` if a new entity type is returned.
4. Add the handler method to `handler.go`.
5. Register the route in `route.go` with the correct HTTP method and middleware.
6. Audit-log the action inside the service transaction.

---

## 24. Database Migrations

Migrations are managed by `golang-migrate/migrate`. They are automatically applied at container startup via the `migrate` service in `docker-compose.yml`.

- `000001_schema.up.sql` — full schema, all tables, indexes, triggers.
- `000002_seed_data.up.sql` — roles, exam types, provinces, wards, reference data.

To add a schema change:
1. Run `make mig-create name=<description>` to generate empty `.up.sql` and `.down.sql` files.
2. Write idempotent SQL in the `.up.sql` file.
3. Write the corresponding rollback SQL in the `.down.sql` file.
4. **Never modify existing migration files** after they have been applied to production.

Useful commands:
```bash
# Run all pending migrations
make mig-up

# Rollback migrations
make mig-down

# Restart DB without wiping volume
make db-reup

# Reset database (destroys all data)
docker compose down -v && docker compose up -d
```

---

## 25. Verification Commands

Run before considering any change complete:

```bash
gofmt -l ./...
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

**The project mandates test files to be placed in a dedicated `tests/` subdirectory within each domain** (e.g. `internal/<domain>/tests/`).
These tests run in a separate package (e.g. `package tests`), enforcing **black-box testing** where only exported identifiers can be tested. We use `go-sqlmock` and the standard `testing` package. Table-driven tests are preferred when the case set is meaningful.

---

## 26. Docker

```bash
# Start all services (PostgreSQL 16, API, Adminer)
docker compose up -d

# Rebuild API after code changes
docker compose up -d --build api

# Tail API logs
docker compose logs -f api

# Reset database (destroys all data)
docker compose down -v
```

Build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api`

Runtime: `alpine:latest`, non-root `appuser`.

---

## 27. Feature Implementation Workflow

For any non-trivial change:

1. Read the affected domain's `service.go`, `entities/`, `handler.go`, `route.go`.
2. Identify the business rules and state transitions involved.
3. Identify concurrent-access risks (registration, payment, seat capacity).
4. Implement the smallest correct change.
5. Add audit logging for state-changing operations.
6. Run `go vet ./...` and `go build ./...`.
7. Run `go test -race ./...` when tests are present.

Do not write code for operations touching `registration`, `payment`, `exam_seats`, or `exam_results` without first tracing the transaction boundaries and understanding the state machine.

---

## 28. What Does NOT Exist in This Project

Do not invent or introduce the following unless explicitly required by a task:

- Redis or any caching layer
- A repository interface layer (services own SQL directly)
- Event bus or message queue
- gRPC
- Structured/JSON logging library (zerolog, zap, slog)
- Dependency injection container (Wire, fx, dig)
- `common/`, `helpers/`, `base/`, `managers/` packages
- Any ORM or query builder
- Clean Architecture layering beyond what is documented here

The existing code is intentionally simple. Preserve that simplicity.

---

## 29. Code Review Priorities

When reviewing any change, check in this order:

1. **Correctness** — does it do the right thing?
2. **Security** — auth, authz, SQL injection, credential exposure?
3. **Data consistency** — transactions, state machines, concurrent access?
4. **Error handling** — are errors handled, wrapped, and mapped correctly?
5. **Audit logging** — is the action logged inside the transaction?
6. **Response envelope** — does the handler use `utils.*` helpers?
7. **Soft-delete** — are all queries filtering `_is_deleted = FALSE`?
8. **Enum validation** — are enum values validated before persistence?
9. **Architecture boundaries** — no business logic in handlers, no HTTP in services?
10. **Tests** — are observable behaviors verified?

---

## 30. Webhook Testing

When developing or debugging third-party webhook integrations (such as payment gateways like VNPAY, Momo, or delivery services), the external service requires a public URL to send requests to.

**Rule:** Always use `localtunnel` to expose your local environment for webhook testing.

**How to use:**
1. Run your local server (usually on port 8080).
2. Start localtunnel via `pnpm`:
   ```bash
   pnpm dlx localtunnel --port 8080
   ```
3. Copy the generated `loca.lt` URL and use it in your payment gateway/webhook configuration (appending the specific API path, e.g., `/api/v1/payments/callback`).
4. Always terminate the tunnel (`Ctrl+C`) when testing is complete to prevent exposing your local development environment to the public internet.
