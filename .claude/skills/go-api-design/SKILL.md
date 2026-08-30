---
name: go-api-design
description: |
  HTTP API contract design and consistency review for the HSK Vinh University
  Registration System backend. Invoke BEFORE or DURING go-feature work to verify
  that a proposed new endpoint conforms to the naming, method, status-code,
  nesting, pagination, and response-DTO conventions already established by the
  ~30 existing routes. Findings-first: this skill reports and proposes; it does
  not modify routes or DTOs. Use go-feature to implement approved changes.
  Fills the gap between go-review (reactive, post-implementation) and go-docs
  (sync-verification) by providing proactive design-time consistency checks.
---

# go-api-design — HTTP API Contract Design and Consistency Review

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-api-design <target>
```

`<target>` may be a proposed new endpoint description (`"POST /admin/exam-schedules"`), a domain being extended (`internal/result/`), or `"full audit"` for a consistency sweep of all existing routes.

This skill **reports findings and proposes corrections only**. It does not modify `route.go`, handler, or DTO files. Use `/go-feature` to implement approved changes.

For implementation-time verification of an already-coded endpoint, see `go-review` Phase 6. For CLAUDE.md route-table sync, see `go-docs` Phase 1 — this skill does not duplicate those checklists.

---

## Baseline Facts (verified against all `route.go` and `dto/` files)

### Route inventory by pattern

| Pattern | Examples found in code |
|---|---|
| Plural noun, kebab-case | `/candidates`, `/exam-sessions`, `/exam-rooms`, `/exam-types`, `/registrations`, `/payments`, `/notifications`, `/locations/provinces`, `/certificates`, `/results` |
| Nested sub-resource (noun) | `/candidates/:id/documents`, `/exam-sessions/:id/seats`, `/certificates/:id/deliveries`, `/locations/provinces/:id/wards` |
| Nested sub-resource (verb — deviation) | `/registrations/:id/confirm`, `/exam-sessions/:id/seats/generate`, `/notifications/:id/read` |
| Flat non-resource (verb path) | `/payments/callback`, `/rechecks`, `/auth/register`, `/auth/login` |
| Admin namespace | `/admin/audit-logs`, `/admin/exam-sessions/:id/seats/generate`, `/admin/results`, `/admin/certificates` |
| Identity singleton | `/me` (GET, PUT, DELETE — no `:id`) |

### Pagination

| Endpoint | Pagination | Response helper |
|---|---|---|
| `GET /admin/audit-logs` | Yes — `?page=&limit=` with `utils.OKWithMeta` | `OKWithMeta` |
| `GET /registrations` | **None** — unbounded array | `utils.OK` |
| `GET /payments` | **None** — unbounded array | `utils.OK` |
| `GET /notifications` | **None** — unbounded array | `utils.OK` |
| `GET /exam-sessions` | **None** — unbounded array | `utils.OK` |
| `GET /exam-rooms` | **None** — unbounded array | `utils.OK` |
| `GET /exam-types` | **None** — unbounded array | `utils.OK` |
| `GET /locations/provinces` | **None** — unbounded array | `utils.OK` |

### Response data on state-change operations

| Endpoint | Data returned |
|---|---|
| `POST /registrations` | Full `ExamRegistrationResponseDTO` object |
| `POST /payments` | Full `PaymentResponseDTO` object |
| `POST /auth/register` | Full `AccountResponseDTO` object |
| `POST /candidates` | Full `CandidateResponseDTO` object |
| `POST /admin/results` | Full `ExamResultResponseDTO` object |
| `POST /rechecks` | Full `RecheckResponseDTO` object |
| `PUT /registrations/:id/confirm` | `gin.H{"message": "registration confirmed"}` ← **string-only** |
| `DELETE /registrations/:id` | `gin.H{"message": "registration cancelled"}` ← **string-only** |
| `PUT /notifications/:id/read` | `gin.H{"message": "notification marked as read"}` ← **string-only** |
| `DELETE /me` | `gin.H{"message": "account deleted"}` ← **string-only** |

### Query parameter convention

| Param | Used by | Form tag casing |
|---|---|---|
| `candidate_id` | `GET /registrations`, `GET /payments` | `snake_case` |
| `actor_id`, `entity_table`, `from`, `to`, `page`, `limit` | `GET /admin/audit-logs` | `snake_case` |

### Response DTO field conventions (verified)

| Convention | Status |
|---|---|
| Primary key: `"id"` | ✓ Consistent across all DTOs |
| Timestamps: `"created_at"`, `"updated_at"` | Do NOT return these to save memory (GC optimization) |
| JSON tag casing: `snake_case` | ✓ Consistent across all DTOs |
| Nullable fields: `omitempty` | ✓ Consistent on pointer types |

---

## Phase 1 — Inspect Before Reviewing

Before evaluating any proposed endpoint, read:

```bash
# All existing route registrations:
grep -rn "\.GET\|\.POST\|\.PUT\|\.DELETE\|\.PATCH" internal/*/route.go | sort

# All response DTO json tags:
grep -rn "json:\"" internal/*/dto/response/*.go | sort

# All query param form tags:
grep -rn "form:\"" internal/*/dto/request/*.go internal/*/handler.go | sort

# Pagination usage:
grep -rn "OKWithMeta\|page\|limit" internal/*/handler.go
```

For each proposed new endpoint, gather: HTTP method, full path, auth requirement, request body fields, response body fields, any query parameters.

---

## Phase 2 — Resource Naming Review

**Established convention (confirmed):** Plural noun, kebab-case path segments. No verbs in path for primary resource routes.

**Rules to enforce for new endpoints:**

| Check | Required | Violation example |
|---|---|---|
| Path segments are plural nouns | Yes | `/result` instead of `/results` |
| Path uses kebab-case | Yes | `/examSessions` instead of `/exam-sessions` |
| No verb in resource-level path | Yes | `/create-payment` instead of `POST /payments` |
| Nested sub-resources use noun segment | Preferred | `/registrations/:id/status` instead of `/registrations/:id/cancel` |

**Existing deviations in the codebase (do not flag these as new bugs — they are established):**

```
PUT  /registrations/:id/confirm   ← verb segment; established pattern
DELETE /registrations/:id         ← correct (method conveys the action)
POST /payments/callback           ← verb; established for gateway callback
PUT  /notifications/:id/read      ← verb; established pattern
POST /admin/exam-sessions/:id/seats/generate  ← verb at end; established
POST /rechecks                    ← non-standard flat resource; established
POST /auth/register               ← verb; standard auth convention
POST /auth/login                  ← verb; standard auth convention
```

**Enforcement rule for NEW endpoints:** If a proposed path contains a verb segment not listed above, flag it as `[MEDIUM] Resource Naming — verb in URL path` and propose the noun-based or method-based alternative.

---

## Phase 3 — Nested vs Flat Resource Pattern

**Established nesting convention (confirmed):** When a sub-resource belongs to exactly one parent resource and the parent ID is required to identify it, use:

```
/parent-resource/:id/child-resource
```

Examples: `/candidates/:id/documents`, `/exam-sessions/:id/seats`, `/certificates/:id/deliveries`, `/locations/provinces/:id/wards`.

**When to use flat routes:** When the sub-resource is queried by the caller's identity (not a path param), the project uses a flat route with a query parameter:

```
GET /registrations?candidate_id=X
GET /payments?candidate_id=X
```

**Decision table for new endpoints:**

| Scenario | Pattern to use |
|---|---|
| Sub-resource identified by parent ID in the URL | Nested: `/parents/:id/children` |
| Collection filtered by the authenticated caller's identity | Flat with query param: `/children?parent_id=X` |
| Same resource queried in both ways | Nested for admin; flat with query param for the owner — do not add both unless there is a clear reason |

**Flag:** Any new flat route using a foreign-key query param for something that has an existing parent resource already in the route table should be reviewed. Prefer the nested form unless the flat form is already established for that relationship.

---

## Phase 4 — Admin Namespace and Authorization Pairing

**Established pattern (confirmed in all four admin routes):**

```go
adminG := r.Group("/admin")
adminG.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
```

Every admin-intent endpoint must have **both** the `/admin` path prefix **and** `RequireRole("admin")` middleware. Neither alone is sufficient.

**Checklist for a proposed new admin endpoint:**

- [ ] Path is under `/admin/...`
- [ ] `JWTAuth(jwtSecret)` applied to the group
- [ ] `middleware.RequireRole("admin")` applied to the same group or handler
- [ ] The endpoint does NOT appear in any public or JWT-only group

**Deviation to flag:** `POST /admin/exam-sessions/:id/seats/generate` uses a route-level group for `RequireRole` rather than the domain-level convention. This is not wrong, but note the pattern difference: most admin routes use `adminG.Use(auth, admin)` where `auth` and `admin` are separate middleware values.

If a new endpoint is described as "admin-only" but the path does not start with `/admin/`, flag as: `[HIGH] Admin Namespace — admin-intent endpoint missing /admin/ prefix`.

---

## Phase 5 — DTO Consistency

**Rule for new DTOs:** Do NOT include `created_at` and `updated_at` in response DTOs or entity structs. This minimizes memory allocation, struct size, and garbage collection overhead. Only select these fields in SQL when explicitly needed for `ORDER BY` or business logic, but do not return them to the client.

## Output Formateign-key field naming

**Convention confirmed:** Foreign key JSON fields use the full domain-qualified name of the referenced entity's ID: `"exam_registration_id"`, `"exam_result_id"`, `"exam_session_id"`, `"exam_seat_id"`, `"candidate_id"`, `"certificate_id"`.

**Rule for new DTOs:** Foreign key fields must use the full reference name, not a shortened alias. Example: use `"exam_session_id"` not `"session_id"` in a new result or registration-related DTO.

**Exception already in code:** `ExamSeatResponseDTO.SessionID` uses `json:"session_id"` — a shortened form. Flag any new DTO that shortens a foreign key field name further. Do not retroactively change `"session_id"` (breaking change).

### Status fields

**Convention confirmed:** All status fields use the key `"status"` (not `"state"`, `"condition"`, or `"phase"`). Follow this for all new DTOs.

### Score field naming in `result` domain

`ExamResultResponseDTO` uses `"listening_score"`, `"reading_score"`, `"writing_score"`, `"total_score"`. `RecheckResponseDTO` uses `"new_total_score"`. The `new_` prefix is unique. If a new DTO needs to return a revised or corrected score, follow the `"new_<field>"` pattern established by `RecheckResponseDTO`.

---

## Phase 6 — HTTP Method and Status Code Consistency

Reference `CLAUDE.md §7` for the response helper list and `go-review` Phase 6 checklist — this skill does not duplicate that table. The focus here is design-time consistency across analogous operations.

**Method selection rules (based on actual usage in this codebase):**

| Intended action | Correct method | Evidence in this repo |
|---|---|---|
| Create a new resource | `POST` | `POST /registrations`, `POST /payments`, `POST /candidates` |
| Replace/update an existing resource | `PUT` | `PUT /candidates/:id`, `PUT /me` |
| Partial update / state transition | `PUT` | `PUT /registrations/:id/confirm`, `PUT /notifications/:id/read` |
| Delete / soft-delete | `DELETE` | `DELETE /registrations/:id`, `DELETE /me` |
| Read a single resource | `GET` | `GET /exam-sessions/:id`, `GET /results/:registration_id`, `GET /certificates/:result_id` |
| Read a list | `GET` | `GET /registrations`, `GET /payments` |
| Admin-only bulk operation | `POST` | `POST /admin/exam-sessions/:id/seats/generate` |

**Flags for new endpoints:**

- `GET` with a body — `[HIGH] HTTP Method — GET must not have a request body`
- Mutating operation modeled as `GET` — `[CRITICAL] HTTP Method — state-changing action must not use GET`
- `PATCH` — `[MEDIUM] HTTP Method — no PATCH in this project; use PUT for partial updates`
- `DELETE` returning a full DTO body — `[LOW] Status Code — DELETE in this project returns gin.H{"message":...}, not a full DTO`

**State-change response body inconsistency (existing, established — apply going forward):**

Four endpoints return `gin.H{"message": "..."}` instead of a DTO: `PUT /registrations/:id/confirm`, `DELETE /registrations/:id`, `PUT /notifications/:id/read`, `DELETE /me`. All new state-change endpoints should follow one of these two patterns — pick whichever the user explicitly requests for the new endpoint, and flag if neither pattern is followed.

---

## Phase 7 — Query Parameter Convention

**Confirmed convention:** `snake_case` form tags. All filter params are optional query strings.

**Established filter param names:**

| Param | Domain | Tag |
|---|---|---|
| `candidate_id` | registration, payment | `form:"candidate_id"` |
| `actor_id` | audit | `form:"actor_id"` |
| `entity_table` | audit | `form:"entity_table"` |
| `from`, `to` | audit (date range) | `form:"from"`, `form:"to"` |
| `page`, `limit` | audit | `form:"page,default=1"`, `form:"limit,default=20"` |

**Rules for new list endpoints:**

- Use `snake_case` for all query parameter names — no `camelCase`, no `PascalCase`.
- For filtering by owner identity (the same concept as `candidate_id`), use `<entity>_id` form — e.g., a new endpoint filtering by account would use `account_id`, not `accountId` or `userId`.
- For pagination on new list endpoints that could return large result sets, add `?page=&limit=` using `form:"page,default=1"` and `form:"limit,default=20"`, and return `utils.OKWithMeta`. Do not add pagination to bounded lists (e.g., reference data like locations/provinces, exam types) where the total count is known to be small.

---

## Phase 8 — Pagination Consistency

**Established pagination shape (from `audit.List` — the only paginated endpoint):**

```go
// Request:
Page  int `form:"page,default=1"`
Limit int `form:"limit,default=20"`   // clamped to 100 in service

// Response (via utils.OKWithMeta):
{
  "success": true,
  "data": [...],
  "meta": {
    "page": 1,
    "per_page": 20,
    "total_items": 150,
    "total_pages": 8
  }
}
```

**Unbounded list endpoints (existing — do not retroactively paginate without explicit request):**

`GET /registrations`, `GET /payments`, `GET /notifications`, `GET /exam-sessions`, `GET /exam-rooms`, `GET /exam-types`, `GET /locations/provinces` — all return unbounded arrays via `utils.OK`. These are established. Do not change them without explicit approval (breaking change for any client relying on the full-array response).

**Rule for new endpoints:**

| List endpoint type | Pagination required? |
|---|---|
| User-scoped list (a candidate's own registrations/payments) | No — bounded by user's activity |
| Admin list with arbitrary filter (like audit logs) | Yes — use `OKWithMeta` |
| Reference data (types, rooms, locations/provinces) | No — bounded by design |
| Unbounded admin list (e.g., all registrations, all candidates) | Yes — use `OKWithMeta` |

Flag a new admin list endpoint that returns an unbounded array as: `[MEDIUM] Pagination — admin list endpoint returns unbounded array; use OKWithMeta with page/limit`.

---

## Phase 9 — Breaking Change Awareness

The API is versioned only by the static `/api/v1` prefix. There is no `/api/v2`. Changing an existing route or DTO field is a breaking change.

**Breaking changes that require explicit approval before implementation:**

| Change type | Risk |
|---|---|
| Rename or remove an existing route path | Breaks clients pointing to old path |
| Change an existing route's HTTP method | Breaks clients using old method |
| Remove a JSON field from a response DTO | Breaks clients reading that field |
| Rename a JSON field in a response DTO | Breaks clients reading old field name |
| Change a required request field to a different name | Breaks clients sending old field |
| Add a new `required` field to an existing request DTO | Breaks clients not sending the new field |

**Additive changes that are safe without approval:**

- Adding a new route that does not conflict with existing paths.
- Adding a new optional field (`omitempty`) to an existing response DTO.
- Adding a new optional query parameter to an existing list endpoint.

**When a breaking change is proposed:** Flag as `[CRITICAL] Breaking Change — <description>`. Do not implement. Require the user to explicitly confirm the change and whether a version bump (`/api/v2`) is needed.

---

## Output Format

Use the same severity format as `go-review` so findings from both skills are visually consistent:

```
[SEVERITY] Category — Short description
Location: internal/<domain>/route.go:<line> or dto/response/<file>.go:<line>
Evidence: <what the code actually does>
Issue: <why this deviates from the established convention>
Proposed fix: <specific, minimal correction>
Requires approval: yes/no — <reason if yes>
```

Severity levels: `CRITICAL` | `HIGH` | `MEDIUM` | `LOW` | `INFO`

Group findings by: Naming → Nesting → Admin Namespace → Method/Status → DTO Fields → Pagination → Breaking Changes.

---

## Do Not

- Modify `route.go`, `handler.go`, or any `dto/` file — this skill reports and proposes; use `/go-feature` to implement.
- Re-run `go-review` Phase 6 checks — reference them by name and defer to that skill for implementation-correctness verification.
- Re-run `go-docs` Phase 1 sync checks — reference them by name and defer to that skill for CLAUDE.md route-table accuracy.
- Propose GraphQL, gRPC, or OpenAPI/Swagger tooling — OpenAPI generation is `go-docs` Phase 5's approval-gated proposal (CLAUDE.md §28).
- Propose a new API versioning scheme (`/api/v2`) without the user explicitly requesting a breaking change and approving a version bump (CLAUDE.md §28).
- Retroactively "fix" established path deviations (`/payments/callback`, `/rechecks`, `/auth/login`) — they are historical convention; flag them only if a new endpoint attempts to copy a non-standard pattern without it being established.
- Remove or rename existing JSON response fields without a `[CRITICAL] Breaking Change` finding and explicit user approval.
- Introduce `PATCH` — it is not used in this project; partial updates use `PUT` (CLAUDE.md §28 implicitly, consistent with existing routes).
