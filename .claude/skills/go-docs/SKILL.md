---
name: go-docs
description: |
  Documentation-sync workflow for the HSK Vinh University Registration System
  backend. Prevents CLAUDE.md from drifting from the actual codebase as features
  are added. Cross-checks the route table (§7), enum reference (§10), and state
  machine diagrams (§18) against live code. After any go-feature run, produces
  the exact CLAUDE.md diff needed — not a general reminder. Findings-only skill:
  does not modify CLAUDE.md autonomously. Reports discrepancies and generates the
  patch text for user approval.
---

# go-docs — Documentation Sync Workflow

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-docs <target>
```

`<target>` may be `"routes"` (cross-check §7), `"enums"` (cross-check §10), `"state machines"` (cross-check §18), `"full sync"` (all three), or `"post-feature"` (run after a `go-feature` completes, check only the changed domain).

This skill **produces findings and a concrete CLAUDE.md patch only**. It does not autonomously modify `CLAUDE.md`. The user must approve the patch before it is applied.

---

## Baseline Facts (verified against actual code)

| Item | Actual state |
|---|---|
| CLAUDE.md route table location | §7 — lists all routes with method, path, auth level |
| CLAUDE.md enum reference location | §10 — complete table with domain, type, and all values |
| CLAUDE.md state machine location | §18 — registration, payment, and seat state diagrams |
| Route files | One `route.go` per domain under `internal/<domain>/route.go` |
| Enum files | One `<domain>_enum.go` per domain under `internal/<domain>/enum/` |
| Service state transitions | Embedded in `service.go` as SQL `UPDATE ... SET status=` and Go status checks |
| OpenAPI / Swagger | **Absent** — CLAUDE.md is the single hand-maintained API documentation |
| CI documentation check | **Absent** — no automated doc-drift detection |

---

## Phase 1 — Route Table Sync (cross-check CLAUDE.md §7)

### Step 1: Extract all registered routes from code

```bash
grep -rn "GET\|POST\|PUT\|PATCH\|DELETE" internal/*/route.go | grep "\." | sort
```

This produces the authoritative list of routes in code.

### Step 2: Compare against CLAUDE.md §7

Read `CLAUDE.md` lines covering §7 (the route table). For each route in code, verify it appears in the table with the correct:
- HTTP method
- Path (including `:id` parameters)
- Auth level (public / JWT / JWT + admin)

For each route in the CLAUDE.md table, verify it exists in code.

### Step 3: Report discrepancies

```
## Route Discrepancies Found

### In code but not in CLAUDE.md:
- [METHOD] [path] — found in internal/<domain>/route.go:<line>

### In CLAUDE.md but not in code:
- [METHOD] [path] — listed in CLAUDE.md §7 but no matching route.go entry
```

### Step 4: Generate the patch

Produce the exact text to add/remove from the route table in CLAUDE.md §7. Example:

```diff
-| location  | GET | `/provinces` | public |
+| location  | GET  | `/provinces`           | public |
+| location  | GET  | `/provinces/:id/wards` | public |
```

Do not apply the patch without user approval.

---

## Phase 2 — Enum Reference Sync (cross-check CLAUDE.md §10)

### Step 1: Extract all enum values from code

```bash
for f in internal/*/enum/*.go; do
    echo "=== $f ==="
    grep -E "^\s+(.*)\s+[A-Z][a-zA-Z]+\s+=\s+\"" "$f" | sed 's/.*= "//' | sed 's/".*//'
done
```

Or more precisely — read every `<domain>_enum.go` file and list each type and its const values.

### Step 2: Compare against CLAUDE.md §10

For each domain/type in the enum table, verify:
- Every `const` value in code is listed in the table.
- No value in the table has been removed from code.
- No new type exists in code but is absent from the table.

### Step 3: Report discrepancies

```
## Enum Discrepancies Found

### New value in code, missing from CLAUDE.md §10:
- Domain: certificate, Type: DeliveryStatus, Value: "returned"
  Found in: internal/certificate/enum/certificate_enum.go:23

### Value in CLAUDE.md §10 but removed from code:
- Domain: payment, Type: PaymentStatus, Value: "on_hold"
  Not found in: internal/payment/enum/payment_enum.go
```

### Step 4: Generate the patch

Produce the exact table row additions/removals for CLAUDE.md §10.

---

## Phase 3 — State Machine Sync (cross-check CLAUDE.md §18)

### Step 1: Extract status transitions from service code

```bash
# Find all status assignment patterns in service files:
grep -rn "status='[a-z_]*'" internal/*/service.go
grep -rn "Status.*= regenum\.\|Status.*= payenum\.\|Status.*= examenum\." internal/*/service.go
```

Also extract enum-based status checks:
```bash
grep -rn "status == regenum\.\|status == payenum\.\|pay\.PaymentStatus !=" internal/*/service.go
```

### Step 2: Compare against CLAUDE.md §18 state diagrams

For each state machine (Registration, Payment, Seat):

Map all transitions found in service code and verify each appears in the diagram. A transition is: `old_status → new_status`, triggered by which service method.

**Current known transitions (verified against `registration/service.go` and `payment/service.go`):**

| Domain | Transition | Method | Diagram in §18 |
|---|---|---|---|
| Registration | `(new) → pending_payment` | `ReserveSeat` | ✓ |
| Registration | `pending_payment → confirmed` | `ConfirmRegistration` | ✓ |
| Registration | `pending_payment → cancelled` | `CancelRegistration` | ✓ |
| Registration | `confirmed → cancelled` | `CancelRegistration` (not blocked) | Verify §18 |
| Payment | `pending → success` | `ProcessCallback` | ✓ |
| Payment | `pending → failed` | `ProcessCallback` | ✓ |
| Payment | `pending → refunded` | `ProcessCallback` | ✓ |
| Seat | `available → held` | `ReserveSeat` | ✓ |
| Seat | `held → booked` | `ConfirmRegistration` | ✓ |
| Seat | `held → available` | `CancelRegistration` | ✓ |
| Seat | `booked → available` | `CancelRegistration` | ✓ (verify in §18) |

### Step 3: Report discrepancies

```
## State Machine Discrepancies Found

### Transition in code but not in CLAUDE.md §18:
- Seat: booked → available — triggered by CancelRegistration
  Found in: registration/service.go:196-200
  Missing from: CLAUDE.md §18 Seat state diagram

### Transition in CLAUDE.md §18 but not enforced in code:
- Registration: confirmed → cancelled — listed as terminal in §18
  But CancelRegistration does not check for confirmed status before cancelling
```

---

## Phase 4 — Post-Feature Sync (run after go-feature)

After any `go-feature` run completes, run this phase to produce the exact CLAUDE.md patch needed.

### Inputs needed

- Which domain was modified or added.
- Which files were created or changed.

### Checklist

```
[ ] New routes added? → Update §7 route table
[ ] New enum values added? → Update §10 enum reference table  
[ ] New enum type added? → Add row to §10
[ ] State machine changed (new status, new transition)? → Update §18
[ ] New service dependency added (new auditSvc injection)? → Update §4 DI section
[ ] New domain created? → Add to §2 project structure, §22 new domain checklist
[ ] New config variable added? → Update §5 config rules
[ ] New shared utility added? → Update §6 or §15 as appropriate
[ ] New middleware added? → Update §14 middleware table
```

### Output format

Produce the precise text to add/change in CLAUDE.md for each item above. Example:

```
## Required CLAUDE.md Update

### §7 Route Table — add:
| newdomain | POST | `/admin/newdomain` | JWT + admin |
| newdomain | GET  | `/newdomain/:id`   | JWT |

### §10 Enum Reference — add row:
| newdomain | `NewThingStatus` | `active`, `inactive`, `archived` |

### §2 Project Structure — add to internal/ list:
├── newdomain/              # New thing management and lifecycle
```

Present this as a diff block the user can review before approving.

---

## Phase 5 — OpenAPI / Swagger Decision

**Current state:** CLAUDE.md is the single hand-maintained API documentation. No OpenAPI tooling exists.

**If the user requests OpenAPI generation:**
- Adding `swaggo/swag` or `ogen` introduces a new dependency to `go.mod`.
- This requires explicit user approval.
- **Default behavior without approval:** Keep CLAUDE.md as the doc. Maintain it through this skill's sync workflow after every feature.

If approval is granted, the correct integration point is:
- `swag init` run as a CI step, not at application startup.
- Generated `docs/swagger.json` committed to the repo.
- A `GET /swagger/*any` route added to the root `engine` in `main.go` using `gin-swagger`.

**Do not implement without explicit approval.**

---

## Verification Commands

```bash
# Extract all routes from code for manual comparison:
grep -rn "GET\|POST\|PUT\|DELETE\|PATCH" internal/*/route.go | grep "\." | sort

# Extract all enum values:
grep -rn "= \"" internal/*/enum/*.go | grep -v "//"

# Extract all status transitions:
grep -rn "status='\|= regenum\.\|= payenum\.\|= examenum\.\|= certenum\.\|= resenum\." \
    internal/*/service.go | sort

# Count routes in CLAUDE.md §7 vs code:
echo "CLAUDE.md routes:"
grep -c "| auth\|| candidate\|| exam\|| registration\|| payment\|| result\|| certificate\|| notification\|| location\|| audit" CLAUDE.md
echo "Code routes:"
grep -rn "\.GET\|\.POST\|\.PUT\|\.DELETE" internal/*/route.go | grep -v "//" | wc -l
```

---

## Do Not

- Modify `CLAUDE.md` without explicit user approval of the proposed patch.
- Introduce OpenAPI tooling (`swag`, `ogen`, `swaggo`) without explicit approval (CLAUDE.md §28 — not part of the current stack).
- Auto-generate `CLAUDE.md` from code — it is a curated constitutional document, not a generated artifact.
- Skip this skill after a `go-feature` run — CLAUDE.md drift is a maintenance debt that compounds.
- Use a documentation framework (Docusaurus, GitBook) without explicit approval (CLAUDE.md §28).
- Report "CLAUDE.md should be updated" without providing the exact text change — always produce the concrete patch.
