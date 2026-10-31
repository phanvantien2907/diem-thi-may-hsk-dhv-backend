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
| Redis | Redis 7 (`redis:7-alpine`) via Docker compose — Caching, distributed lock (`internal/cache`) |
| Image validation | Google Cloud Vision API (`cloud.google.com/go/vision/apiv1 v1.2.0`) |
| Dependency injection | Manual constructor wiring in `cmd/api/main.go` |
| Build | Multi-stage Docker, `CGO_ENABLED=0`, stripped (`-s -w`), UPX compressed (Target < 30MB runtime layer, < 55MB disk usage) |
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
├── candidate/              # Candidate profile, identity documents, admin review/listing, metrics, and PDF export
├── certificate/            # Certificate issuance and delivery
├── config/                 # Configuration loading (config.go)
├── dashboard/              # Admin dashboard analytics, live sessions, suspicious payments, manual actions
├── database/               # PostgreSQL connection pool (postgres.go)
├── exam/                   # Exam types, sessions, seat generation (room data is internal-only)
├── exception/              # RESTful unified exception and error response format
├── location/               # Provinces and wards (public reference data)
├── media/                  # Cloudinary file upload/delete service (shared infrastructure)
├── middleware/              # auth.go, cors.go, logger.go, ratelimit.go
├── notification/           # System notifications
├── payment/                # Payment creation and gateway callback handling
├── registration/           # Exam registration and seat reservation
├── result/                 # Exam results and recheck requests
├── utils/                  # Shared HTTP response helpers, validation, password hashing, param parsing
├── worker/                 # Background workers (PayOS expired transaction sync, seat cleanup)
└── ws/                     # WebSocket hub and client connections for real-time dashboard events

migrations/
├── 000001_schema.up.sql          # Full DB schema (applied first)
├── 000001_schema.down.sql        # Rollback for schema
├── 000002_seed_data.up.sql       # Seed / reference data
├── 000002_seed_data.down.sql     # Rollback for seed data
└── 00NN_*.up.sql / *.down.sql    # Incremental schema changes (000018 drops exam_session_duplicated_from_id)

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

The `candidate` domain is documented and tagged as one `Candidate` module. Candidate profile, identity-document, upload-signature, admin review, duplicate-detection, roster, admin listing, metrics, and PDF export endpoints must remain under this module/tag; do not split them into separate Swagger/API modules.

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
- **Raw SQL & PostgreSQL clause rules**:
  - In `INSERT INTO ... RETURNING` and `UPDATE ... RETURNING`, PostgreSQL disallows references to table aliases in `RETURNING` unless the target table is explicitly aliased in the statement (`INSERT INTO table AS t ...` or `UPDATE table t ...`). Always ensure target table alias matches or use unaliased bare column names in `RETURNING` to prevent `missing FROM-clause entry for table "<alias>"` errors.
  - Table name integrity: The registration table is strictly named `exam_registrations` (never `registrations`). Relations follow: `certificates` → `exam_results` (`exam_result_exam_registration_id`) → `exam_registrations` (`exam_registration_candidate_id`) → `candidates` (`candidate_account_id`).
- Payment reconciliation stores the gateway-reported amount in nullable `payment_amount_received`; webhook underpayment uses `partial_paid` and overpayment uses `amount_mismatch` without confirming the registration.
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
| auth | POST | `/admin/accounts/:id/deactivate` | JWT + admin |
| auth | POST | `/admin/accounts/:id/reactivate` | JWT + admin |
| auth | POST | `/admin/staff-accounts` | JWT + admin |
| candidate | POST | `/me/candidate-profile` | JWT |
| candidate | GET | `/me/candidate-profile` | JWT |
| candidate | PATCH | `/me/candidate-profile` | JWT |
| candidate | POST | `/me/candidate-profile/documents` | JWT |
| candidate | GET | `/me/candidate-profile/documents` | JWT |
| candidate | GET | `/me/candidate-profile/upload-signature` | JWT |
| candidate | POST | `/me/candidate-profile/validate-portrait` | JWT |
| candidate | POST | `/me/candidate-profile/clear-cache` | JWT |
| candidate | GET | `/admin/candidates` | JWT + admin |
| candidate | GET | `/admin/candidates/metrics` | JWT + admin |
| candidate | GET | `/admin/candidates/export` | JWT + admin |
| candidate | GET | `/admin/candidates/duplicates` | JWT + admin |
| candidate | POST | `/admin/candidates/merge` | JWT + admin |
| candidate | POST | `/admin/candidates/clear-cache` | JWT + admin |
| candidate | GET | `/admin/candidates/:id` | JWT + admin |
| candidate | DELETE | `/admin/candidates/:id/lock` | JWT + admin |
| candidate | POST | `/admin/candidates/:id/approve` | JWT + admin |
| candidate | POST | `/admin/candidates/:id/reject` | JWT + admin |
| candidate | POST | `/admin/candidates/:id/revoke-approval` | JWT + admin |
| candidate | POST | `/admin/candidates/:id/reopen-review` | JWT + admin |
| candidate | GET | `/admin/exam-sessions/:id/roster` | JWT + admin |
| exam | GET | `/exam-types` | public |
| exam | GET | `/exam-sessions` | public |
| exam | GET | `/exam-sessions/:id` | public |
| exam | GET | `/exam-sessions/:id/seats` | public |
| exam | POST | `/exam-sessions/clear-cache` | public |
| exam | GET | `/admin/exam-periods` | JWT + admin |
| exam | POST | `/admin/exam-periods` | JWT + admin |
| exam | GET | `/admin/exam-periods/:id` | JWT + admin |
| exam | PUT | `/admin/exam-periods/:id` | JWT + admin |
| exam | DELETE | `/admin/exam-periods/:id` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/duplicate` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/publish` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/unpublish` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/close-registration` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/reopen-registration` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/reschedule` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/complete` | JWT + admin |
| exam | POST | `/admin/exam-periods/:id/cancel` | JWT + admin |
| exam | GET | `/admin/exam-sessions` | JWT + admin |
| exam | POST | `/admin/exam-sessions/clear-cache` | JWT + admin |
| exam | POST | `/admin/exam-sessions/batch-create` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/seats/generate` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/publish` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/schedule-publish` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/unpublish` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/close-registration` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/reopen-registration` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/capacity` | JWT + admin |
| exam | POST | `/admin/exam-sessions/:id/cancel` | JWT + admin |
| exam | DELETE | `/admin/exam-sessions/:id/draft` | JWT + admin |
| exam | PUT | `/admin/exam-sessions/:id` | JWT + admin |
| registration | POST | `/registrations` | JWT |
| registration | GET | `/registrations?candidate_id=X` | JWT |
| registration | PUT | `/registrations/:id/confirm` | JWT |
| registration | DELETE | `/registrations/:id` | JWT |
| payment | POST | `/payments/payos/create` | JWT |
| payment | POST | `/payments/callback` | public |
| payment | GET | `/payments?candidate_id=X` | JWT |
| payment | GET | `/admin/payments` | JWT + admin |
| payment | GET | `/admin/payments/export` | JWT + admin |
| payment | POST | `/admin/payments/:id/confirm` | JWT + admin |
| payment | POST | `/admin/payments/:id/sync` | JWT + admin |
| payment | POST | `/admin/payments/:id/cancel` | JWT + admin |
| payment | POST | `/admin/payments/:id/mark-refund` | JWT + admin |
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
| dashboard | GET | `/admin/metrics/overview` | JWT + admin |
| dashboard | GET | `/admin/metrics/revenue-chart` | JWT + admin |
| dashboard | GET | `/admin/metrics/hsk-distribution` | JWT + admin |
| dashboard | GET | `/admin/metrics/payment-funnel` | JWT + admin |
| dashboard | GET | `/admin/exam-sessions/live-status` | JWT + admin |
| dashboard | GET | `/admin/payments/suspicious` | JWT + admin |
| dashboard | POST | `/admin/payments/:id/manual-approve` | JWT + admin |
| dashboard | POST | `/admin/payments/:id/refund-mark` | JWT + admin |
| dashboard | GET | `/admin/logs/recent` | JWT + admin |
| dashboard | POST | `/admin/system/clear-cache` | JWT + admin |
| dashboard | GET | `/admin/users/me/audit-log` | JWT + admin |
| ws | GET | `/ws/dashboard` | JWT + admin |

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

Candidate admin list and export query conventions:

- `GET /admin/candidates` supports optional `status`, `level`, `search`, `exam_date`, `page`, and `limit`.
- `status` accepts `all`, `pending`, `verified`, or `rejected`; omitted/`all` means all candidates. `level` and `search` are limited to 100 characters.
- `exam_date` uses `YYYY-MM-DD` and filters candidates by the exam session date.
- `GET /admin/candidates/export` requires `exam_date`; it exports only candidates registered for that exam date and may receive the same `status`, `level`, and `search` filters.
- The export response is a streamed `application/pdf` attachment and is not wrapped in the JSON success envelope. Its filename is `ds_thi_sinh_{dd-mm-yyyy}.pdf` for all, `ds_ho_so_thi_sinh_cho_duyet_{dd-mm-yyyy}.pdf` for pending, `ds_ho_so_thi_sinh_da_duyet_{dd-mm-yyyy}.pdf` for verified, and `ds_ho_so_thi_sinh_tu_choi_{dd-mm-yyyy}.pdf` for rejected. The PDF embeds Noto Serif SC directly (serif/Times New Roman-style, with Vietnamese and Chinese glyphs) and must preserve UTF-8 text; it must not depend on fonts installed in the runtime image.
- Candidate admin list/export `status` filters accept `all`, `pending`, `verified`, or `rejected`; omitted/`all` means all candidates. Rejected PDF rows include `Từ chối: <rejection_reason>` in the notes column; when no reason exists, the note is `-`.
- Export preserves the original exam type name, excludes HSK 1–2, and truncates overlong cell values with `...` to preserve table alignment.
- Admin candidate list/detail responses include the complete candidate profile, account contact fields, latest exam level, and the complete latest identity document. Nullable fields are represented as explicit JSON `null` values. `target_exam_level` currently mirrors the latest registered exam type because the schema has no separate target-level column.
- The admin candidate response includes `full_name_cn` (`candidate_full_name_cn`) for the candidate's Chinese name; it is returned as `null` when not provided.
- Candidates update this field through `PATCH /api/v1/me/candidate-profile` using the exact JSON key `full_name_cn`; the service persists it to `candidates.candidate_full_name_cn` and preserves the existing value when the field is omitted. If a client accidentally sends one extra JSON-encoding layer (for example the literal value `"潘文进"` including quote characters), the service removes that single outer layer before persistence.
- Front-end clients must send `full_name_cn` as a normal UTF-8 Unicode string (for example `潘文进`) and must not call `JSON.stringify()` on that field separately. `PingFang SC` is only a DataGrip/macOS display font; it is not required by the API or database.

### Portrait Image Validation

When candidates upload identity documents via `POST /me/candidate-profile/documents`, the portrait image (`portrait_image_url`) is automatically validated using Google Cloud Vision API. The validation runs synchronously during document upload and sets the `candidate_document_verification_status` accordingly.

**Validation Rules:**

1. **Input Validation** (before calling Vision API):
   - URL must not be empty → "URL ảnh không được để trống!"
   - URL must be valid format → "URL ảnh không hợp lệ!"

2. **Face Detection**:
   - Exactly one face must be detected → "phải có chính xác một khuôn mặt trong ảnh!"
   - No glasses or headwear (checks `POSSIBLE`, `LIKELY`, and `VERY_LIKELY` likelihoods) → "không được đeo kính hoặc trang phục che khuôn mặt!"

3. **Image Quality**:
   - Image must not be blurred → "ảnh bị mờ, không rõ nét!"
   - Brightness must be between 80-240 (out of 255) → "ảnh quá tối, không đủ sáng!" or "ảnh quá sáng, bị cháy sáng!"

4. **Face Geometry**:
   - Roll angle < 20° (rotation around camera axis)
   - Pan angle < 25° (rotation around vertical axis)
   - Tilt angle < 20° (rotation around horizontal axis)
   - If any angle exceeds threshold → "góc chụp khuôn mặt không hợp lệ (đầu nghiêng quá nhiều)!"

5. **Background Color**:
   - Background must be white (RGB within threshold of 30 from 255,255,255 for dominant color)
   - If dominant color is not white, checks top 3 dominant colors for at least one white color
   - This handles cases where the subject (skin/clothes) dominates but background is white
   - If not white → "nền ảnh phải màu trắng!"

6. **Safe Search**:
   - Image must not contain explicit content (adult, violence, medical, racy)
   - If unsafe → "ảnh chứa nội dung không phù hợp!"

**Behavior:**

- If validation passes → stored in database with status `verified` (HTTP 201 Created)
- If validation fails → **action blocked immediately** with HTTP 400 Bad Request and error message detailing all rejection reasons; **no database record is persisted**
- If Vision API is not configured or fails → status saved as `pending` (HTTP 201 Created) for manual admin review
- Graceful degradation: system continues to work without Vision API, but documents require manual approval

**Configuration:**

- Vision API credentials are configured via `GOOGLE_CLOUD_CREDENTIALS_PATH` environment variable
- The credentials file should be a Google Cloud service account JSON key placed in the `secrets/` directory
- Docker Compose mounts the secrets directory at `/app/secrets` in the container

**API Call Optimization:**

- The implementation uses `AnnotateImage` with batch features (FACE_DETECTION, IMAGE_PROPERTIES, SAFE_SEARCH_DETECTION) in a single API call
- This reduces API cost and latency compared to multiple separate calls

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
PAYMENT, REFUND, CANCEL_SUCCESS_PAYMENT,
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
| exam | `ExamPeriodStatus` | `draft`, `published`, `registration_open`, `registration_closed`, `completed`, `cancelled` |
| exam | `ExamSessionStatus` | `draft`, `scheduled`, `open`, `closed`, `completed`, `cancelled` |
| exam | `ExamSessionClosedReason` | `manual`, `full`, `deadline`, `period_closed` |
| exam | `ExamSeatStatus` | `available`, `held`, `booked` |
| registration | `ExamRegistrationStatus` | `pending_payment`, `confirmed`, `cancelled` |
| payment | `PaymentMethod` | `vnpay`, `momo`, `zalopay`, `bank_transfer`, `cash`, `payos` |
| payment | `PaymentStatus` | `pending`, `success`, `failed`, `refunded`, `expired`, `cancelled`, `amount_mismatch`, `partial_paid` |
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

Both `fn_book_exam_seat` and `fn_transfer_registration` first delegate to
`fn_assert_session_accepts_registration(p_session_id, p_candidate_id)`, which
takes `SELECT ... FROM exam_sessions ... FOR SHARE` on the target session (and
`FOR SHARE` on its parent `exam_periods`) and validates the **effective**
status, the registration deadline, the effective capacity, and duplicates.
`FOR SHARE` is deliberate: closing registration writes to the same session row,
so the two operations serialise — nobody can slip a booking in at the exact
moment an admin closes registration. The function raises named plpgsql
exceptions (`EXAM_SESSION_NOT_FOUND`, `EXAM_SESSION_CANCELLED`,
`EXAM_SESSION_COMPLETED`, `EXAM_SESSION_NOT_PUBLISHED`,
`EXAM_PERIOD_CANCELLED`, `REGISTRATION_CLOSED`, `DUPLICATE_REGISTRATION`,
`NO_SEAT_AVAILABLE`) which
`registration.mapBookingError` translates into Vietnamese messages.

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
confirmed       → cancelled   (admin cancel success before exam date)
cancelled       → (terminal)
```

A cancelled registration cannot be confirmed. Cancellation frees the seat to `available`.

### Payment

```
pending → success   (via PayOS callback, PayOS sync, or admin confirm; confirms registration and books seat)
pending → failed
pending → cancelled (admin cancel or expiry worker; releases registration and seat)
pending → partial_paid (webhook amount is lower than required; no slot confirmation)
pending → amount_mismatch (webhook amount is higher than required; no slot confirmation)
success → refunded   (admin cancel before exam date or mark-refund)
```

`expired` is the worker's terminal timeout state for legacy/automated expiry
records. The service rejects any callback for a non-`pending` payment. Successful
confirmation and PayOS sync lock the payment, registration, and seat rows with
`FOR UPDATE` before changing state. Amount mismatches store
`payment_amount_received` and never confirm the registration.

Admin cancel locks the payment, registration, exam session, and seat in one
ACID transaction. If `exam_session_date <= CURRENT_DATE` (the exam has passed
or is in progress), it rejects and rolls back without changing payment,
registration, or seat. A seat is released only when its current status is
`held` or `booked`; Redis is not used. Successful-payment cancellation records
`CANCEL_SUCCESS_PAYMENT` with the admin actor inside the same transaction.

### Seat

```
available → held    (15-minute window, during reservation)
held      → booked  (on payment success / registration confirm)
held      → available (on cancellation or expiry)
booked    → available (on admin cancellation before exam date)
booked    → available (on registration cancellation)
```

### Exam period

```
(new)  → draft
draft  → published            (needs ≥ 1 valid child session)
published → registration_open (automatic: now() ≥ exam_period_registration_start)
registration_open → registration_closed  (deadline reached, or admin closes early)
registration_closed → registration_open  (admin reopens before exam date; a new future deadline is required)
registration_open|registration_closed → completed
draft|published|registration_open|registration_closed → cancelled
```

`exam_period_status` persists `draft`, `published`, `registration_open`,
`registration_closed`, `completed`, `cancelled`. The window between
`exam_period_registration_start` and `exam_period_registration_deadline`
distinguishes `published` from `registration_open`.

### Exam session

```
(new)   → draft
draft   → open        (publish immediately; requires a date and ≥ 1 seat)
draft   → scheduled   (schedule publish; requires a future exam_session_publish_at)
scheduled → open      (publish_at elapsed, parent period effectively registration_open)
scheduled → draft     (unpublish — cancels the schedule; or parent period closed)
open    → closed      (closed_reason = manual | full | deadline | period_closed)
closed  → open        (reopen; see conditions below)
open|closed → cancelled
open|closed → completed  (exam day passed)
```

`exam_session_status` has six values: `draft`, `scheduled`, `open`, `closed`,
`completed`, `cancelled`. `exam_session_publication_status` **no longer exists** —
the old publication states are folded into `exam_session_status`.

### Exam session `closed_reason`

`closed_reason` is set if and only if `status = 'closed'`
(db constraint `ck_exam_session_closed_reason_required`). "Đã đóng" and
"Đã hết hạn" are two renderings of the same `closed` status, distinguished only
by the reason. The reason also decides whether the session can be reopened:

| `closed_reason` | Reopen allowed when |
|---|---|
| `manual` | always (parent period must be effectively `registration_open`) |
| `full` | capacity was increased above the registered count first |
| `deadline` | a new future `exam_session_registration_deadline` is supplied |
| `period_closed` | never individually — the parent period must be reopened |

### Effective status (authoritative)

Stored status columns lag behind wall-clock time, so **never trust
`exam_session_status` / `exam_period_status` for a business decision**.
`internal/exam/statemachine.go` derives the effective state from `now()`:

- `ExamPeriodStatus.Effective(now)`: `draft`/`cancelled`/`completed`/
  `registration_closed` short-circuit; otherwise `published` before
  `registration_start`, `registration_closed` after the deadline, else
  `registration_open`.
- `SessionState.Effective(now) (status, closedReason)`: short-circuits on
  `draft`/`cancelled`/`completed`; a passed exam day yields `completed`; a stored
  `closed` yields `closed` plus its reason; a stored `scheduled` stays `scheduled`
  until its `publish_at` elapses *and* the parent period is effectively open;
  then the parent period gates the session (`cancelled`/`completed` propagate,
  only `registration_open` lets it proceed, anything else closes it with
  `period_closed`); then its own deadline (`deadline`), then capacity (`full`),
  else `open`.
- The cron (`worker/exam_publication_cron.go` → `Service.SyncStoredStatuses`)
  only writes the stored column so lists and dashboards render correctly.

### Column-const aliasing (exam service)

`sessionColumns` and `periodColumns` in `internal/exam/service.go` emit
`es.`/`ep.`-qualified names, so **every statement that uses them must alias its
target table**. `RETURNING` resolves against the DML target's scope, so
`UPDATE exam_sessions RETURNING es.exam_session_id` fails with
`missing FROM-clause entry for table "es"`. Always write
`UPDATE exam_sessions es SET ...`, `INSERT INTO exam_sessions AS es (...)`,
`UPDATE exam_periods ep SET ...`, `INSERT INTO exam_periods AS ep (...)`.
A new DML statement using these consts without the alias is a runtime 500 that
compiles, vets and `go test` pass — sqlmock only matches the parameterised
prefix, not `RETURNING`. Verify any new statement against real PostgreSQL with
`PREPARE stmt AS <sql>; DEALLOCATE stmt;` (a `PREPARE` failure is exactly the
error the API would return), or hit the endpoint and confirm a 2xx.

### Exam session duplication was removed

`POST /admin/exam-sessions/:id/duplicate` and
`request.DuplicateExamSessionRequestDTO` no longer exist, and the column
`exam_session_duplicated_from_id` was dropped by migration
`000018_remove_exam_session_duplication`. Do not reintroduce a session-level
copy: duplicating a **period** (`POST /admin/exam-periods/:id/duplicate`) is the
only supported way to pre-fill a new event, and it always regenerates child
sessions in `draft`. `ActionDuplicate` therefore remains valid for periods but
is **not** part of `allSessionActions` — `GuardSessionAction` denies it for every
session status, and `available_actions` on a session never contains `duplicate`.

### Shared guard

Every mutating exam endpoint calls `GuardPeriodAction` / `GuardSessionAction`
and aborts with HTTP **409** and `code: "INVALID_STATE_TRANSITION"` (plus a
Vietnamese reason) when the action is not allowed for the effective state.
`AvailablePeriodActions` / `AvailableSessionActions` are returned to the client
as `available_actions` so the UI can mirror the same matrix. The action matrix
is the one documented in the business spec: editing is full for `draft`/
`published`, description-only for `registration_open`, and denied afterwards;
publish requires ≥ 1 valid child session; unpublish from `registration_open` is
allowed only with 0 registrations; reschedule is allowed for `published`/
`registration_open`/`registration_closed`; delete is `draft`-only.

Session-level rules: `publish`/`schedule_publish` only from `draft`/`scheduled`
**and only when the parent period is not in `draft`** (parent must be `published`,
`registration_open`, or `registration_closed`; if the parent period is `draft`,
`publish` and `schedule_publish` are omitted from `available_actions` and denied with
409 "Đợt thi đang ở trạng thái nháp; phải công bố đợt thi trước khi công khai ca thi");
`unpublish` from `scheduled`, or from `open` with 0 registrations;
`reopen_registration` from `closed` only when the parent period is effectively
`registration_open`, the exam day has not passed, and `closed_reason` is
`manual` or `deadline` (a `full` session must be widened first, a
`period_closed` session can only be reopened via its period);
`increase_capacity`/`decrease_capacity` from `open`/`closed`;
`check_in` from `closed` on the exam day or from `completed`;
`cancel` from `open`/`closed`; `delete_draft` from `draft`/`scheduled`.

`internal/exam/tests/status_matrix_test.go` asserts this matrix cell by cell for
both state machines, and `internal/exam/tests/cascade_test.go` covers every
cascade path. Any change to a denial rule must update those tables.

### Exam period ↔ session cascade (single transaction)

- **Close period registration** — child sessions with effective `open` become
  `closed` + `period_closed`; children already closed for another reason keep
  their reason; `scheduled` children are cancelled back to `draft` with
  `exam_session_publish_at = NULL`; `draft` children are never published.
- **Reopen period registration** — only children with
  `closed_reason = 'period_closed'` return to `open`; `full`/`manual`/`deadline`
  children stay closed. A new future deadline is required.
- **Cancel period** — every child session becomes `cancelled`; each registration
  must resolve to either refund or transfer (`resolution` = `refund` |
  `transfer`, `target_session_id` required for `transfer`).
- **Reschedule period** — no status change. Writes
  `exam_period_original_date`, increments `exam_period_reschedule_count`, shifts
  every child session date and notifies candidates.

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

### Ordering rules inside a migration

`migrate` runs a whole migration file as one multi-statement `ExecContext`, so PostgreSQL
executes it inside an **implicit transaction**. If any statement fails the entire file
rolls back — but `schema_migrations` is still left `dirty = true`.

Because of that, statement order matters and must be:

1. Add new columns.
2. **Drop / widen the existing `CHECK` constraint.**
3. Backfill data that needs the widened values.
4. Add the stricter `CHECK` constraint that codifies the new rule.
5. Drop obsolete columns and constraints.

Adding the new constraint before the backfill, or backfilling before dropping the old
constraint, fails on a database that already contains rows (it passes on an empty one —
always test migrations against a clone of real data, not an empty database).

### Replay safety

A migration may be executed a second time — for example after `schema_migrations` is
manually rewound, or when the same version is applied to a database whose schema is
already up to date. Guard every `UPDATE`/`SELECT` that reads a column the same file
drops, otherwise the replay fails with `column "..." does not exist`:

```sql
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name   = 'exam_sessions'
          AND column_name  = 'exam_session_publication_status'
    ) THEN
        UPDATE exam_sessions SET ... WHERE exam_session_publication_status = 'draft';
    END IF;
END $$;
```

DDL statements are already replay-safe when they use `ADD COLUMN IF NOT EXISTS`,
`DROP CONSTRAINT IF EXISTS` before every `ADD CONSTRAINT`, and
`CREATE INDEX IF NOT EXISTS`. Backfills that reference soon-to-be-dropped columns are
the only part that needs an explicit existence guard.

### Recovering from a dirty migration

```bash
# 1. Read the failure reason
docker compose logs migrate | tail -30

# 2. Inspect what actually committed. DDL is transactional, so usually nothing —
#    but verify: if every column/constraint/index from the migration IS present,
#    the version DID commit and you must set version = <that_version>.
docker compose exec postgres psql -U postgres -d hsk_vinh -c "SELECT version, dirty FROM schema_migrations;"
docker compose exec postgres psql -U postgres -d hsk_vinh -c "\d exam_sessions"

# 3. Fix the offending migration file (see "Replay safety" above), then reset the flag:
#    - migration did NOT commit  -> version = <previous_version>
#    - migration DID commit      -> version = <failed_version>
docker compose exec postgres psql -U postgres -d hsk_vinh \
  -c "UPDATE schema_migrations SET version = <version>, dirty = false;"

# 4. Re-run only the failed migration
make db-build
```

Never `UPDATE schema_migrations SET dirty = false` without first fixing the SQL —
`migrate` would skip the file and the schema would silently stay out of date.

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
Candidate admin listing, metrics, and PDF export behavior is covered by `internal/candidate/tests/admin_features_test.go`, including filter/pagination SQL arguments, metrics counts, exam-date validation, PDF generation, and handler validation.

Exam status behavior is covered by three black-box suites in `internal/exam/tests/`:
`statemachine_test.go` (guard + effective-status units), `status_matrix_test.go`
(the full period/session action matrix, `closed_reason` reopen rules, drift edge
cases, and the absence of session `duplicate`), `cascade_test.go` (period
complete/unpublish/cancel/reschedule/close cascades with sqlmock), and
`handler_state_test.go` (the HTTP 409 `INVALID_STATE_TRANSITION` envelope, 400
and 404 mappings).

---

## 26. Docker & Container Size Optimization Rules

### Common Commands
```bash
# Start all services (PostgreSQL 16, API, Adminer, Redis, Ngrok)
docker compose up -d

# Rebuild API after code changes (fast, cached, stripped & UPX compressed)
docker compose up -d --build api

# Tail API logs
docker compose logs -f api

# Reset database (destroys all data)
docker compose down -v
```

### Build Specifications & Size Budget (< 100MB Hard Limit)
To prevent container image bloat (historical bloat exceeded 200MB), all Docker builds and any new service containers MUST adhere to the following rules:

1. **Stripped & Compressed Binary:**
   - Compiler flags: `CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api`
   - Executable compression: `upx --best --lzma /bin/api`
   - This compresses the binary from ~56MB down to **18MB** (a 68% size reduction).
2. **Multi-Architecture Support:**
   - Do NOT hardcode `GOARCH=amd64`. Use `ARG TARGETOS TARGETARCH` to natively compile for both `linux/amd64` and `linux/arm64` (macOS Apple Silicon).
3. **Runtime Minimal Image:**
   - Runtime base: `alpine:3.21` (or latest stable Alpine minimal).
   - Run as non-root `appuser`.
   - Resulting image: **~22.3MB content transfer / 51MB disk usage**, well below the 100MB ceiling.
4. **Build Context Hygiene (`.dockerignore`):**
   - Local build binaries (`bin/`), secrets (`secrets/`), migrations (`migrations/`), tests, `.git/`, and temp files must always be excluded in `.dockerignore`.
   - Never allow local artifacts to inflate the build context or leak credentials into image layers.
5. **Logging Rotation for All Compose Services:**
   - Every service defined in `docker-compose.yml` must configure a log rotation driver (`max-size: "10m"`, `max-file: "3"`) so container logs never cause host disk exhaustion.
6. **Auxiliary Containers:**
   - Any new container/service added to `docker-compose.yml` must use an Alpine-based image tag (e.g., `redis:7-alpine`, `postgres:16-alpine`) and avoid heavyweight Debian/Ubuntu full images.


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

- **Redis or caching layers** — Strictly forbidden for Dashboard analytics. All dashboard metrics (`overview`, `revenue-chart`, `hsk-distribution`, `payment-funnel`, `live-status`) query PostgreSQL directly in **100% Real-time** mode, backed by specialized partial & compound indexes.
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

**Rule:** Use the integrated `ngrok` tunnel in `docker-compose.yml` to expose your local environment for webhook testing.

**How to use:**
1. Get your free static domain and authtoken from [ngrok.com](https://ngrok.com/).
2. Add them to your `.env` file:
   ```env
   NGROK_AUTHTOKEN=your_ngrok_auth_token_here
   NGROK_DOMAIN=your_ngrok_static_domain_here.ngrok-free.app
   ```
3. Start your docker-compose stack:
   ```bash
   docker-compose up -d
   ```
4. Configure PayOS/Webhook providers to use your static Ngrok domain (e.g., `https://your-domain.ngrok-free.app/api/v1/payments/callback`).

---

## 31. Front-End Integration Guidelines (Tài liệu Tích hợp Front-end)

Khi Front-end kết nối với Backend, đặc biệt trong các luồng nghiệp vụ nhạy cảm như Đăng ký thi (FlashSale) và Thanh toán, FE CẦN tuân thủ các quy tắc sau:

### 13.1. Xử lý lỗi Đăng ký (HTTP 409 Conflict)
Khi thí sinh bấm Đăng ký (`POST /api/v1/me/registrations`), Backend sử dụng Pessimistic Locking để cấp phát ghế.
- **Nếu thành công (201 Created)**: FE nhận được `id` của phiếu đăng ký. Lập tức chuyển sang màn hình Thanh toán.
- **Nếu thất bại (409 Conflict)**: Ghế đã được người khác lấy hoặc đã hết.
  - **Quy tắc FE**: KHÔNG spam lại request liên tục (tránh Rate Limit 429). Phải hiển thị Toast/Modal báo lỗi rõ ràng: "Rất tiếc, đã hết ghế trống cho ca thi này!" và khóa nút Đăng ký.

### 13.2. Luồng Thanh toán (Payment Flow)
Sau khi có `exam_registration_id`, FE gọi `POST /api/v1/payments/payos/create`.
- **Nếu thành công (200/201)**: Nhận `checkoutUrl`.
  - **Quy tắc FE**: Lập tức chuyển hướng (Redirect) người dùng sang trang của PayOS, HOẶC mở Iframe PayOS để người dùng quét mã QR.
- **Polling trạng thái thanh toán**:
  - Khi Webhook PayOS gọi về Backend, Backend sẽ đổi trạng thái phiếu đăng ký sang `paid`.
  - **Quy tắc FE**: FE cần thiết lập cơ chế Polling (gọi `GET /api/v1/me/registrations` mỗi 3-5 giây) để kiểm tra xem `status` đã đổi thành `paid` chưa. Nếu thành `paid`, lập tức hiển thị màn hình Chúc mừng và dừng Polling. (Tương lai có thể dùng WebSocket/SSE nếu cần Real-time).

### 13.3. Xử lý Timeout (Flash Sale Overwhelm)
Trong các đợt thi nóng (hàng chục ngàn người online), Backend có thể mất > 10 giây để trả lời request đăng ký do cơ chế Xếp hàng (Queueing).
- **Quy tắc FE**: 
  - Set HTTP Timeout của Axios/Fetch tối thiểu là **15 giây** (Không set quá ngắn gây lỗi Timeout oan).
  - Khi người dùng bấm Đăng ký, hiển thị màn hình Loading (Loading Spinner hoặc "Đang xếp hàng... Vui lòng không tải lại trang") và **vô hiệu hóa (disable)** nút Đăng ký để chặn double-click.

### 13.4. Định dạng dữ liệu (Data Types)
- Các trường ID (`id`, `exam_session_id`) trong Backend là `int64`. Tuy JavaScript hỗ trợ số lớn (BigInt), FE nên lưu ý khi làm việc với JSON để tránh tràn số (Precision loss).
- Luôn gửi JWT Token vào header `Authorization: Bearer <token>`. Nếu nhận `401 Unauthorized`, FE phải tự động điều hướng người dùng về trang Đăng nhập.

### 13.5. Quy tắc hiển thị ca thi

- API public của module `exam` chỉ trả về ca có **trạng thái hiệu lực** là `open` (xem mục 18). Trạng thái hiệu lực được tính từ `now()`, hạn chót đăng ký, ngày thi, `exam_session_publish_at` và trạng thái hiệu lực của đợt cha — **không** đọc thẳng cột `exam_session_status` đã lưu.
- Response trả về cả `status` (trạng thái hiệu lực) và `stored_status` (giá trị đã lưu trong DB) để FE đối chiếu khi cột lưu bị trễ.
- `draft` không hiển thị công khai và không nhận `exam_session_publish_at`.
- `scheduled` bắt buộc có `exam_session_publish_at` là thời điểm trong tương lai.
- Worker nền của module `exam` chỉ đồng bộ cột trạng thái đã lưu để hiển thị (`SyncStoredStatuses`): `scheduled` đến hạn (và đợt cha đang mở đăng ký) → `open` + `exam_session_publish_at = NULL`.
- `closed` là trạng thái duy nhất cho cả “đã đóng” và “đã hết hạn”; FE phân biệt bằng `closed_reason` (`manual`, `full`, `deadline`, `period_closed`) và ẩn/hiện nút hành động theo `available_actions`.
- `available_actions` đồng bộ chặt chẽ với trạng thái đợt thi cha: khi đợt thi cha mang trạng thái `draft` (hoặc `cancelled`, `completed`), backend tự động loại bỏ `publish` và `schedule_publish` khỏi `available_actions` của ca thi con, ngăn FE hiển thị các nút thao tác không hợp lệ.
- Mọi hành động không hợp lệ trả HTTP 409 kèm `code: "INVALID_STATE_TRANSITION"` và `msg` giải thích lý do bằng tiếng Việt.
- `exam_session_check_in_at` là thời điểm có mặt dạng RFC 3339 và có thể để trống.
- API `exam` không còn endpoint hoặc field response về phòng máy; dữ liệu `exam_rooms` và khóa DB cũ chỉ được giữ nội bộ để tương thích registration.

---

## 32. Real-time Dashboard & Index Performance Rules

`GET /api/v1/admin/metrics/overview` thuộc module và Swagger tag `Dashboard`, cùng nhóm với các endpoint dashboard còn lại. Không tách endpoint này thành module/tag riêng.

1. **Strictly Real-time (No Caching):**
   - All Dashboard analytics endpoints (`/admin/metrics/overview`, `/admin/metrics/revenue-chart`, `/admin/metrics/hsk-distribution`, `/admin/metrics/payment-funnel`, `/admin/exam-sessions/live-status`) MUST query PostgreSQL directly in real-time.
   - Do NOT introduce in-memory or Redis caches for dashboard metrics.
   - The user/admin must always see 100% live, authoritative numbers whenever the dashboard is viewed or refreshed.

2. **Index-Driven Performance:**
   - To prevent database bottlenecks under concurrent admin access, performance is guaranteed through database index design:
     - `idx_payments_dashboard_perf` on `payments(payment_status, payment_exam_registration_id, payment_amount)` where `payment_is_deleted = FALSE`.
     - `idx_exam_registrations_dashboard_perf` on `exam_registrations(exam_registration_exam_session_id, exam_registration_id)` where `exam_registration_is_deleted = FALSE`.
     - `idx_audit_logs_actor_created` on `audit_logs(audit_log_actor_account_id, audit_log_created_at DESC)` for fast personal audit logs ("My Log").
     - `idx_audit_logs_created_at` on `audit_logs(audit_log_created_at DESC)` for system recent activity.

3. **"My Log" (`GetMyAuditLog`):**
   - The personal action history endpoint (`GET /api/v1/admin/users/me/audit-log`) is strictly filtered by the authenticated administrator's ID (`audit_log_actor_account_id = $1`) with pagination (`page`, `limit`).
   - It is kept completely independent of system-wide audit logs and must not be altered when adjusting other analytics features.

---

## 33. WebSocket Real-time Event Hub (`internal/ws`)

1. **Architecture:**
   - `ws.Hub` maintains active WebSocket connections authenticated via admin JWT token.
   - Mounted at `GET /api/v1/ws/dashboard`.
   - Thread-safe registration, unregistration, and broadcast mechanisms using Go channels and goroutines.

2. **Supported Events:**
   - `payment_confirmed`: Triggered when an admin manually approves a transaction (`POST /admin/payments/:id/manual-approve`).
   - `dashboard_metrics_updated`: Triggered when background workers sync and expire stale payments or release held seats, alerting connected clients to refresh view.

3. **Client Integration:**
   - Frontend admins connect on dashboard mount and automatically receive live events to update UI state without continuous polling.

---

## 34. Background Housekeeping Workers (`internal/worker`)

1. **Responsibilities:**
   - Runs ticker-based background tasks to maintain data consistency:
     - Expire pending PayOS payments that exceeded the 15-minute payment window.
     - Release expired seat reservations back to `available` status atomically.
   - Triggers real-time WebSocket broadcast notifications whenever stale payments/seats are reclaimed.

2. **Concurrency Rules:**
   - Workers must respect graceful shutdown using `context.Context`.
   - Database operations in workers must be wrapped in atomic transactions (`tx.BeginTx`) with proper locking and error logging.

---

## 35. Swagger API Documentation (`make sg`)

- Whenever route handlers, Swagger annotations (`// @Summary`, `// @Router`), or DTO structs are added or modified:
  ```bash
  make sg
  ```
  This runs `swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal` to recompile Swagger specifications.
- Swagger UI is served at `/swagger/index.html`.
