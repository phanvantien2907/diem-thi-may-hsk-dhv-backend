---
name: go-database
description: |
  Database workflow for the HSK Vinh University Registration System backend.
  Invoke when writing, reviewing, or debugging SQL queries, schema migrations,
  indexes, transactions, or connection management. Uses raw database/sql with
  lib/pq driver and PostgreSQL 16. No ORM. Migrations are plain SQL files.
  Covers query patterns, locking, pagination, and schema conventions specific
  to this project.
---

# go-database — Database Workflow

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-database <target>
```

`<target>` may be a domain, a specific query, "new migration", "schema review", or a description.

---

## Baseline Facts

- Driver: `github.com/lib/pq` via standard `database/sql`.
- Database: PostgreSQL 16.
- No ORM, no query builder — all SQL is written by hand.
- Placeholders: `$1`, `$2`, ... (PostgreSQL positional style). Never `?`.
- Migrations: plain `.sql` files under `migrations/`, applied alphabetically at container start by `docker-entrypoint-initdb.d`.
- Schema: `001_schema.sql`. Seed data: `002_seed_data.sql`. Next change: `003_*.sql`.

---

## Phase 1 — Inspect Before Writing

Before any database work:

```bash
# Understand the existing schema
cat migrations/001_schema.sql

# Understand naming, column prefix conventions, existing tables
grep -n "CREATE TABLE" migrations/001_schema.sql

# Understand existing query patterns in the affected domain
cat internal/<domain>/service.go
```

---

## Phase 2 — Schema Conventions

Every table must follow these conventions exactly:

```sql
CREATE TABLE IF NOT EXISTS <table_name>s (
    -- Primary key
    <table>_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Business columns (ALL prefixed with table name)
    <table>_<col>      <TYPE> NOT NULL,
    <table>_<opt_col>  <TYPE>,          -- nullable optional

    -- Soft-delete (required on every table)
    <table>_is_deleted BOOLEAN      NOT NULL DEFAULT FALSE,
    <table>_deleted_at TIMESTAMPTZ,

    -- Timestamps (required on every table)
    <table>_created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    <table>_updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- Constraints
    CONSTRAINT uq_<table>_<col> UNIQUE (<table>_<col>)
);

-- Soft-delete-aware index (include WHERE clause)
CREATE INDEX IF NOT EXISTS idx_<table>_<col>
    ON <table_name>s (<table>_<col>)
    WHERE <table>_is_deleted = FALSE;

-- auto-update trigger (reuse the existing function)
CREATE OR REPLACE TRIGGER trg_<table>_updated_at
    BEFORE UPDATE ON <table_name>s
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('<table>_updated_at');
```

**Critical rules:**
- Every column is prefixed by its table name (`exam_seat_status`, not `status`).
- Every table has `_is_deleted`, `_deleted_at`, `_created_at`, `_updated_at`.
- Every table has the `fn_touch_updated_at` trigger.
- Primary keys are `BIGINT GENERATED ALWAYS AS IDENTITY`.
- Foreign key columns are `BIGINT NOT NULL REFERENCES <other_table> (<pk>)`.

---

## Phase 3 — Query Patterns

### Single row insert with RETURNING

```go
var e SomeEntity
err := tx.QueryRowContext(ctx, `
    INSERT INTO some_things
           (some_thing_field_a, some_thing_field_b)
    VALUES ($1, $2)
    RETURNING some_thing_id, some_thing_field_a, some_thing_field_b,
              some_thing_is_deleted, some_thing_deleted_at,
              some_thing_created_at, some_thing_updated_at`,
    valueA, valueB,
).Scan(
    &e.SomeThingID, &e.SomeThingFieldA, &e.SomeThingFieldB,
    &e.SomeThingIsDeleted, &e.SomeThingDeletedAt,
    &e.SomeThingCreatedAt, &e.SomeThingUpdatedAt,
)
```

### Multiple rows

```go
rows, err := s.db.QueryContext(ctx, `
    SELECT col_a, col_b
    FROM some_things
    WHERE some_thing_owner_id = $1
      AND some_thing_is_deleted = FALSE
    ORDER BY some_thing_created_at DESC`, ownerID)
if err != nil {
    return nil, fmt.Errorf("domain: list: %w", err)
}
defer rows.Close()

var list []SomeEntity
for rows.Next() {
    var e SomeEntity
    if err := rows.Scan(&e.ColA, &e.ColB); err != nil {
        return nil, fmt.Errorf("domain: scan: %w", err)
    }
    list = append(list, e)
}
return list, rows.Err()  // always check rows.Err()
```

### Update with RowsAffected check

```go
res, err := tx.ExecContext(ctx, `
    UPDATE some_things
    SET    some_thing_status     = $1,
           some_thing_updated_at = NOW()
    WHERE  some_thing_id = $2
      AND  some_thing_is_deleted = FALSE`, newStatus, id)
if err != nil {
    return fmt.Errorf("domain: update: %w", err)
}
if n, _ := res.RowsAffected(); n == 0 {
    return errors.New("some_thing not found")
}
```

### Soft delete

```go
res, err := tx.ExecContext(ctx, `
    UPDATE some_things
    SET    some_thing_is_deleted = TRUE,
           some_thing_deleted_at = NOW(),
           some_thing_updated_at = NOW()
    WHERE  some_thing_id = $1
      AND  some_thing_is_deleted = FALSE`, id)
```

Never use `DELETE` for application data — always soft-delete.

### Pagination

```go
// Use LIMIT + OFFSET with a total count
var total int
_ = s.db.QueryRowContext(ctx,
    `SELECT COUNT(*) FROM some_things WHERE some_thing_is_deleted = FALSE`,
).Scan(&total)

rows, err := s.db.QueryContext(ctx, `
    SELECT ...
    FROM some_things
    WHERE some_thing_is_deleted = FALSE
    ORDER BY some_thing_created_at DESC
    LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
```

Return pagination metadata using `utils.OKWithMeta(c, items, &meta)`.

---

## Phase 4 — Migrations

### Adding a new migration

1. Check the highest existing migration number:
   ```bash
   ls migrations/
   ```
2. Create `migrations/00N_description.sql` (next number).
3. Write idempotent SQL only:
   ```sql
   CREATE TABLE IF NOT EXISTS ...
   CREATE INDEX IF NOT EXISTS ...
   ALTER TABLE some_things ADD COLUMN IF NOT EXISTS ...
   ```
4. **Never alter `001_schema.sql` or `002_seed_data.sql`** after production deployment.
5. New tables must include all schema conventions from Phase 2.
6. New columns on existing tables need a `DEFAULT` value or must be nullable.

### Apply migration locally

```bash
docker compose down -v && docker compose up -d
```

---

## Phase 5 — Index Strategy

Add indexes based on actual query patterns — not speculation.

| When to add | Index type |
|---|---|
| Foreign key column queried frequently | `CREATE INDEX IF NOT EXISTS idx_<table>_<fk> ON <table> (<fk>) WHERE <table>_is_deleted = FALSE` |
| Status column filtered in hot queries | Partial index on status + soft-delete |
| Unique business key (code, ref number) | `CONSTRAINT uq_... UNIQUE` or partial unique index |
| Full-text search | Not present in this project — do not add unless required |

Do not add indexes "just in case". Every index slows down writes.

---

## Phase 6 — Connection Management

The `*sql.DB` connection pool is configured in `internal/database/postgres.go`. Do not tune
pool settings inside domain service code. If pool configuration needs changing, modify
`postgres.go` only.

Do not open new `*sql.DB` connections inside service methods.

---

## Phase 7 — Checklist for Any DB Change

- [ ] All new tables follow naming and column prefix conventions.
- [ ] All new tables have `_is_deleted`, `_created_at`, `_updated_at`, `fn_touch_updated_at` trigger.
- [ ] All queries use `$N` positional placeholders.
- [ ] All queries filtering existing rows include `AND *_is_deleted = FALSE`.
- [ ] All multi-row queries have `defer rows.Close()` and `rows.Err()` check.
- [ ] All single-row inserts use `RETURNING` and scan immediately.
- [ ] All updates that must affect one row check `RowsAffected()`.
- [ ] All atomic operations use `BeginTx` + `defer Rollback`.
- [ ] Migration file is numbered correctly and idempotent.
- [ ] New indexes are based on real query patterns, not speculation.
- [ ] No `DELETE` statements on application data — use soft-delete.

---

## Do Not

- Use an ORM or query builder.
- Use `?` placeholders — PostgreSQL requires `$N`.
- Write SQL in handler files — SQL belongs in `service.go`.
- Modify `001_schema.sql` or `002_seed_data.sql` after initial deployment.
- Add speculative indexes.
- Hardcode connection strings — use `DATABASE_URL` from config.
