---
name: go-testing
description: |
  Testing workflow for the HSK Vinh University Registration System backend.
  Invoke when writing, expanding, or fixing tests for any domain. The project
  currently has no existing test files. Uses standard Go testing package, same-package
  tests, table-driven style. Covers unit, integration, and HTTP handler layers.
  Use this skill for all test authoring and test failure diagnosis.
---

# go-testing — Testing Workflow

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-testing <target>
```

`<target>` may be a domain name (`registration`), a specific file (`internal/payment/service.go`), or a description (`"all registration state transitions"`).

---

## Baseline Facts

- The project has **no existing test files**. There are no established test helpers to inherit.
- Testing package: standard library `testing` and `go-sqlmock`.
- Test location: dedicated `tests/` subdirectory inside each domain (`internal/<domain>/tests/`).
- Test package name: `package tests` (implying black-box testing; only exported identifiers can be tested).
- Test file naming: `<subject>_test.go` (e.g., `service_test.go`, `handler_test.go`, `enum_test.go`).
- Preferred style: **table-driven tests** when testing multiple cases of the same behavior.
- Use `go-sqlmock` for isolating DB dependencies in service and handler tests.

---

## Phase 1 — Understand What to Test

Read the implementation first:
```bash
cat internal/<domain>/service.go
cat internal/<domain>/handler.go
cat internal/<domain>/mapper.go
cat internal/<domain>/entities/*_entities.go
cat internal/<domain>/dto/request/*_request.go
cat internal/<domain>/dto/response/*_response.go
cat internal/<domain>/enum/<domain>_enum.go
```

Identify:
- All exported service methods.
- All business rules enforced (state machine transitions, capacity guards, duplicate checks).
- All validation points (DTO struct tags, `.Valid()` enum checks, `RowsAffected` checks).
- All error paths (not found, conflict, infrastructure).
- All concurrency-sensitive operations (seat reservation, payment callbacks).

---

## Phase 2 — Choose the Right Testing Level

| Level | When to use | What it tests |
|---|---|---|
| **Unit** | Pure logic: enum `.Valid()`, mapper functions, `utils.Validator` | No DB, no HTTP |
| **Service (integration)** | Business rules, transactions, state machines | Requires a real DB connection or a well-isolated fake |
| **Handler (HTTP)** | Request binding, validation, status codes, response envelope | Uses `httptest`, calls handler directly |

For this project, **handler tests** are the most practical starting point because:
- Services embed SQL directly — no repository interface to mock.
- The `audit.Service` is the only interface in the codebase and can be faked.

---

## Phase 3 — Coverage Requirements

Every test target must cover:

| Scenario | Required? |
|---|---|
| Happy path (valid input, success) | Always |
| JSON binding failure | Handler tests |
| Struct validation failure (missing/invalid field) | Handler tests |
| Enum validation failure | Service tests |
| Not found (`sql.ErrNoRows`) | Service + handler tests |
| Business rule violation (duplicate, cancelled state) | Service tests |
| Concurrent duplicate requests | Where applicable (registration, payment callback) |
| Authorization failure (missing token, wrong role) | Handler tests |
| Infrastructure/DB error | Service tests where feasible |

---

## Phase 4 — Table-Driven Test Pattern

```go
func TestServiceDoThing(t *testing.T) {
    tests := []struct {
        name    string
        input   SomeInput
        wantErr bool
        wantMsg string
    }{
        {
            name:  "happy path",
            input: SomeInput{Field: "valid"},
        },
        {
            name:    "missing required field",
            input:   SomeInput{},
            wantErr: true,
            wantMsg: "field is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := svc.DoThing(context.Background(), tt.input)
            if tt.wantErr {
                if err == nil {
                    t.Fatalf("expected error, got nil")
                }
                if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
                    t.Errorf("error %q does not contain %q", err.Error(), tt.wantMsg)
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            _ = got // assert fields as needed
        })
    }
}
```

---

## Phase 5 — Handler Test Pattern

Use `net/http/httptest`. Test the handler function directly without starting a real server.

```go
func TestHandlerDoThing(t *testing.T) {
    // Set up a minimal Gin engine for testing
    gin.SetMode(gin.TestMode)

    tests := []struct {
        name       string
        body       string
        wantStatus int
    }{
        {
            name:       "happy path",
            body:       `{"field":"value"}`,
            wantStatus: http.StatusCreated,
        },
        {
            name:       "invalid JSON",
            body:       `{invalid`,
            wantStatus: http.StatusBadRequest,
        },
        {
            name:       "missing required field",
            body:       `{}`,
            wantStatus: http.StatusBadRequest,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            w := httptest.NewRecorder()
            c, r := gin.CreateTestContext(w)
            r.POST("/test", handler.DoThing)

            req := httptest.NewRequest(http.MethodPost, "/test",
                strings.NewReader(tt.body))
            req.Header.Set("Content-Type", "application/json")
            c.Request = req

            r.ServeHTTP(w, req)

            if w.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d; body = %s",
                    w.Code, tt.wantStatus, w.Body.String())
            }
        })
    }
}
```

---

## Phase 6 — Faking audit.Service

`audit.Service` is the only exported interface in the project. Use a fake in service tests:

```go
type fakeAudit struct{ logErr error }

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ request.CreateAuditLogRequestDTO) error {
    return f.logErr
}
func (f *fakeAudit) List(_ context.Context, _ request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error) {
    return response.AuditLogListResponseDTO{}, nil
}
```

---

## Phase 7 — Enum and Mapper Unit Tests

These require no DB and should be tested exhaustively:

```go
func TestPaymentStatusValid(t *testing.T) {
    valid := []PaymentStatus{PayStatusPending, PayStatusSuccess, PayStatusFailed, PayStatusRefunded}
    for _, s := range valid {
        if !s.Valid() {
            t.Errorf("%q should be valid", s)
        }
    }
    if PaymentStatus("garbage").Valid() {
        t.Error("garbage should not be valid")
    }
}
```

---

## Phase 8 — Run Tests

```bash
go test ./internal/<domain>/...
go test -race ./internal/<domain>/...
go test -v ./internal/<domain>/...   # for diagnosis
go test ./...                         # full suite
go test -race ./...                   # full suite with race detector
```

---

## Phase 9 — Diagnose Failures

For each failure:
1. Read the full error output.
2. Locate the specific assertion.
3. Check whether the failure is in the test or the implementation.
4. Only fix the implementation if there is a clear bug. Do not alter tests to paper over bugs.
5. Report failures that cannot be fixed without a change in requirements.

---

## Do Not

- Use any mocking framework not already in the project.
- Test implementation details — test observable behavior (return values, DB state, HTTP status codes).
- Write tests that merely call functions and assert no error without checking the result.
- Introduce `time.Sleep` in tests.
- Skip the race detector for concurrency-sensitive code (registration, payment).
