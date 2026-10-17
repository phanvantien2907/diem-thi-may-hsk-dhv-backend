---
name: go-deploy
description: |
  CI/CD and deployment workflow for the HSK Vinh University Registration System
  backend. Establishes a GitHub Actions CI pipeline (the repo currently has no
  .github/ directory), documents Docker image build conventions consistent with
  the existing multi-stage Dockerfile, defines environment promotion using the
  existing config.go mechanism, and explicitly documents migration safety at
  deploy time — including the critical limitation that docker-entrypoint-initdb.d
  only runs on a fresh volume and how to safely apply new migrations to an
  already-running production database.
---

# go-deploy — CI/CD and Deployment Workflow

**Source of truth for project conventions: `CLAUDE.md` at the repository root.**

---

## Invocation

```
/go-deploy <target>
```

`<target>` may be `"ci pipeline"`, `"migration"`, `"docker build"`, `"rollback"`, `"env promotion"`, or `"full setup"`.

This skill **implements changes to CI/CD files only** — it never modifies application source code, `CLAUDE.md`, or existing skill files.

---

## Baseline Facts (verified against actual repo)

| Item | Actual state |
|---|---|
| CI pipeline | **ABSENT** — no `.github/` directory exists |
| Dockerfile | Multi-stage: `golang:1.23-alpine` builder → `alpine:3.21` runtime; non-root `appuser`; `CGO_ENABLED=0` static binary; UPX compressed (< 30MB layer content, ~51MB disk usage) |
| Docker Compose | PostgreSQL 16 + API + Adminer + Redis 7 + Ngrok; API `depends_on: migrate (completed), redis (started)`; JSON log rotation enabled |
| Migration mechanism | Plain `.sql` files mounted to `docker-entrypoint-initdb.d/` — **only runs on a fresh volume** |
| Current migrations | `001_schema.sql`, `002_seed_data.sql` |
| Config mechanism | `godotenv` + `os.LookupEnv`; `mustGetEnv` for required vars; `.env` file for local dev |
| `.env` gitignore | `.env`, `.env.local`, `.env.*.local` all gitignored ✓ |
| Health endpoint | `GET /health` on root engine — suitable for CI smoke test |

---

## Phase 1 — GitHub Actions CI Pipeline

**Current state:** No CI exists. The `.github/` directory does not exist.

**Create the following file:**

```
.github/workflows/ci.yml
```

The pipeline runs the exact same commands already established by `go-feature` Phase 5, `go-refactor` Phase 6, and `CLAUDE.md §25` — no new check types:

```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

jobs:
  test:
    name: Build and Test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - name: Format check
        run: |
          if [ -n "$(gofmt -l .)" ]; then
            echo "The following files are not gofmt-formatted:"
            gofmt -l .
            exit 1
          fi

      - name: Vet
        run: go vet ./...

      - name: Build
        run: go build ./...

      - name: Test (with race detector)
        run: go test -race ./...

      - name: Vulnerability scan
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...
```

**Notes:**
- `govulncheck` requires no new `go.mod` dependency — it is a dev tool.
- The race detector (`-race`) is required for the `registration` and `payment` domains.
- No test database is configured in CI yet. When integration tests are added (see `go-testing`), add a `services: postgres:` block to this job.
- The pipeline branches (`main`, `develop`) should be adjusted to the team's actual branch names.

---

## Phase 2 — Docker Image Build Convention

The existing `Dockerfile` is production-ready. Do not redesign it.

**Image tagging convention (propose for CI):**

```bash
# Build
docker build -t hsk-vinh-api:${GIT_SHA} .
docker build -t hsk-vinh-api:latest .

# In GitHub Actions:
IMAGE_TAG=$(git rev-parse --short HEAD)
docker build -t hsk-vinh-api:${IMAGE_TAG} -t hsk-vinh-api:latest .
```

Add an optional Docker build job to the CI pipeline — only runs on push to `main`:

```yaml
  docker:
    name: Docker Build
    runs-on: ubuntu-latest
    needs: test
    if: github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v4
      - name: Build image
        run: |
          docker build \
            -t hsk-vinh-api:$(git rev-parse --short HEAD) \
            -t hsk-vinh-api:latest \
            .
```

Push to a registry (Docker Hub, GHCR) only if explicitly configured — do not assume a registry is available.

**Key Dockerfile properties to preserve:**
- Multi-arch support: `CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}` static binary.
- `-trimpath -ldflags="-s -w"` for stripped binary.
- `upx --best --lzma /bin/api` for executable compression (< 20MB binary size).
- Non-root `appuser` on minimal `alpine:3.21` runtime.
- `ca-certificates` and `tzdata` copied from builder.
- Strict size budget: API image must remain under 100MB (target <= 30MB layer content, <= 55MB disk usage).

---

## Phase 3 — Environment Promotion

The project uses a single `Config` struct loaded from environment variables. There is no second config library.

**Environment model:**

| Env | `APP_ENV` | `.env` source | Notes |
|---|---|---|---|
| Local dev | `development` | `.env` file (gitignored) | `godotenv.Load()` succeeds |
| CI | `test` or not set | GitHub Actions secrets / env vars | `.env` file absent; `mustGetEnv` reads from env |
| Staging | `staging` | Deployment secrets manager | `.env` file absent |
| Production | `production` | Deployment secrets manager | `gin.SetMode(gin.ReleaseMode)` activated |

**Rules:**
- Never commit a `.env` file. Use `.env.example` as documentation only.
- Inject `DATABASE_URL`, `JWT_SECRET`, and all required vars as environment variables in staging/production.
- `APP_ENV=production` activates `gin.SetMode(gin.ReleaseMode)` — do not skip this.
- Add new required config vars to `.env.example` with a safe placeholder, not a real value.

**Adding a new environment variable checklist:**
1. Add field to `Config` struct in `config.go`.
2. Load with `getEnv(key, default)` or `mustGetEnv(key)`.
3. Add to `.env.example` with placeholder.
4. Add to GitHub Actions secrets and document in the deployment runbook.

---

## Phase 4 — Migration Safety at Deploy Time (CRITICAL)

**The single most dangerous deploy-time risk in this project.**

**How migrations currently work:**

PostgreSQL's `docker-entrypoint-initdb.d/` mechanism **only runs when the data directory is empty** (fresh volume). On an existing volume, mounting new migration files has **no effect**.

```yaml
# docker-compose.yml mounts:
- ./migrations/001_schema.sql:/docker-entrypoint-initdb.d/001_schema.sql:ro
- ./migrations/002_seed_data.sql:/docker-entrypoint-initdb.d/002_seed_data.sql:ro
```

**This means:** Adding `003_add_feature.sql` to `docker-compose.yml` and restarting the postgres container on a production server **will NOT apply the migration**.

**Correct process for shipping a new migration to production:**

```
Step 1: Write the migration file locally
        → migrations/003_description.sql
        → Must be idempotent: CREATE TABLE IF NOT EXISTS, ADD COLUMN IF NOT EXISTS

Step 2: Test locally on a fresh volume
        → docker compose down -v && docker compose up -d
        → Verify the schema change is applied correctly

Step 3: Review the migration SQL
        → Run go-review (or manual review) on the SQL
        → Verify it does not drop columns, rename tables, or break existing queries

Step 4: Apply to production manually
        → Connect: psql $DATABASE_URL
        → Or via Docker: docker exec -i hsk_vinh_postgres psql -U postgres -d hsk_vinh < migrations/003_description.sql
        → Verify: \dt, \d <table>, SELECT count(*) FROM ...

Step 5: Deploy the new API image
        → docker compose up -d --build api  (or update the image tag in production)
        → Verify the app starts cleanly

Step 6: Update docker-compose.yml to mount the new file
        → For future fresh installs only
```

**Migration SQL must always be idempotent:**
```sql
-- Correct:
ALTER TABLE exam_seats ADD COLUMN IF NOT EXISTS exam_seat_notes TEXT;
CREATE INDEX IF NOT EXISTS idx_exam_seats_held_until
    ON exam_seats (exam_seat_held_until)
    WHERE exam_seat_status = 'held' AND exam_seat_is_deleted = FALSE;

-- Never:
ALTER TABLE exam_seats ADD COLUMN exam_seat_notes TEXT;  -- fails if already run
DROP TABLE IF EXISTS ...;  -- never in a forward migration
```

---

## Phase 5 — Rollback Strategy

**There is no migration down-script convention in this project.** Migrations are applied forward-only.

**Rollback definition for this project:**

| What | How |
|---|---|
| Application rollback | Redeploy the previous image tag: `docker compose up -d --build api` or `docker pull hsk-vinh-api:<prev-sha> && docker compose up -d` |
| DB schema rollback | Manual — write a compensating SQL script (e.g., `DROP COLUMN IF NOT EXISTS`) and apply via `psql`. Only do this if the column/table has no data that needs preserving |
| Rollback decision | If the new image fails health check (`GET /health` returns non-200) within 30 seconds of deploy, roll back the image first, then assess the DB |

**CI artifact retention:** Tag every merged image with both `latest` and the short Git SHA so previous versions are always available for redeployment.

**Add to deployment runbook:**
- Keep the last 3 image tags available in the registry.
- Do not delete previous migration SQL files.
- Before applying any additive migration, take a `pg_dump` snapshot.

---

## Verification Commands

```bash
# Validate CI workflow syntax
# Install: npm install -g @actions/toolkit  -- or use GitHub's web editor

# Local Docker build test (mirrors CI)
docker build -t hsk-vinh-api:local .

# Migration dry-run on fresh volume
docker compose down -v && docker compose up -d postgres
docker compose logs postgres | grep "database system is ready"

# Smoke test after deploy
curl -f http://localhost:8080/health | jq .
# Expected: {"success":true,"data":{"status":"ok"},...}
```

---

## Do Not

- Add a migration management framework (golang-migrate, atlas, goose) without explicit approval — these are not present in the stack (CLAUDE.md §28).
- Modify the application `Dockerfile` as part of CI setup — only create `.github/` workflow files.
- Introduce new config libraries (`viper`, `koanf`, etc.) for environment promotion (CLAUDE.md §28 — godotenv is the only config library).
- Apply migration files to production using `docker-entrypoint-initdb.d` — it does not work on existing volumes.
- Tag Docker images with mutable tags only (e.g., `latest` alone) — always also tag with the Git SHA for rollback capability.
- Run `docker compose down -v` on a production server — this destroys all data.
