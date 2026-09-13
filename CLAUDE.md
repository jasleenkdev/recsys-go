# claude.md — working notes for Claude Code sessions

## What this is
recsys-go recommends GitHub repositories and answers grounded questions over their READMEs.
- **Backend (Go, repo root):** HTTP API, Kafka consumer, and ingest CLIs. Uses Postgres + pgvector, Qdrant, Redis, Kafka, a Python embedding sidecar, and Ollama. Deploy target: **Render** (no config committed yet).
- **Frontend (`frontend/`):** Next.js 16 App Router, React 19, and Auth.js v5 with GitHub sign-in. Deploy target: **Vercel**.
- More detail: [architecture.md](architecture.md) (read its Open Questions before any deploy work) and [requirements.md](requirements.md).

## Layout rules
- `cmd/<binary>/main.go`: one binary per folder. All API handlers live in `cmd/api/main.go`.
- `internal/store`: every SQL query and every Qdrant, sidecar, or Ollama HTTP call. Handlers don't write SQL, except `authSyncHandler` and `checkRefs`.
- `internal/config`: the **only** env reader, via `config.Load()`, which is memoized.
- `migrations/NNNNNN_name.{up,down}.sql`: always add both files. The next number is `000017`.
- `api/openapi.yaml` ⇄ `frontend/lib/api.ts` types ⇄ the Go response structs must stay in sync.
- `frontend/app/actions.ts`: the only path from client components to the API.
- `frontend/app/components/`: one component per file, PascalCase.

## Commands

### Backend (run from the repo root, since `.env` is loaded from the CWD)
```bash
cp .env.example .env                      # then adjust
go build ./... && go vet ./...            # both pass today
gofmt -l cmd internal                     # 10 files currently unformatted — gofmt files you touch
go test ./...                             # tests: cmd/consumer (retry/commit), cmd/promote (skips w/o DB)
RECSYS_TEST_DATABASE_URL="$DATABASE_URL" go test ./cmd/promote   # Postgres+pgvector integration; uses a throwaway schema

migrate -path migrations -database "$DATABASE_URL" up   # ASSUMED golang-migrate; tool not referenced in repo

go run ./cmd/api                          # :8081
go run ./cmd/consumer
GITHUB_TOKEN=... go run ./cmd/loader      # ingest step 1: GitHub → staging
go run ./cmd/promote                      # ingest step 2: embed → Postgres (needs sidecar)
go run ./cmd/promote-qdrant               # ingest step 3: Postgres → Qdrant
go run ./cmd/test-candidates              # smoke: rank for user 1
```

### Embedding sidecar
```bash
cd embedding_sidecar
python3.10 -m venv venv && source venv/bin/activate
pip install fastapi uvicorn sentence-transformers    # no requirements.txt; local venv has fastapi 0.141.1, sentence-transformers 5.7.0, torch 2.13.0
uvicorn main:app --port 8000
```

### Ollama
```bash
ollama pull llama3.2:3b && ollama serve   # :11434
```

### Frontend
```bash
cd frontend
cp .env.example .env.local                # fill AUTH_* and RECSYS_API_URL
npm ci
npm run dev                               # :3001 (not 3000)
npm run lint && npx tsc --noEmit          # both clean today
npm run build
```

### Manual infra (no docker-compose in repo)
Postgres with pgvector on 5432, Qdrant on **6343**, Redis on **6390**, Kafka on 9092. Collections and topic must be created by hand. The parameters below are assumed:
```bash
curl -X PUT localhost:6343/collections/repo_embeddings -H 'Content-Type: application/json' -d '{"vectors":{"size":768,"distance":"Cosine"}}'
curl -X PUT localhost:6343/collections/readme_chunks   -H 'Content-Type: application/json' -d '{"vectors":{"size":768,"distance":"Cosine"}}'
kafka-topics.sh --create --topic repo-events --bootstrap-server localhost:9092
```

## Deployment workflow
- **Vercel:** set Root Directory `frontend` and Node 22.x. Set env `AUTH_SECRET`, `AUTH_GITHUB_ID`, `AUTH_GITHUB_SECRET`, and `RECSYS_API_URL` (the Render URL). Point the GitHub OAuth callback at `https://<domain>/api/auth/callback/github`. Push to the connected branch, or run `vercel --prod` from `frontend/`.
- **Render (proposed):** web service `go build -o bin/api ./cmd/api` → `./bin/api`. It listens on Render's injected `PORT` (don't set `API_PORT` there), with health check path `/healthz`. Background worker `go build -o bin/consumer ./cmd/consumer` → `./bin/consumer`. Run migrations before first deploy. A `render.yaml` blueprint is not written yet.
- Before deploying, choose Kafka, Qdrant, and Redis providers and set their auth env vars (see requirements.md), and decide where the sidecar and Ollama run (architecture.md #9).

## Conventions observed

### Go
- Each file starts with a path comment (`// internal/store/items.go`).
- Handlers are factories: `fooHandler(db *sql.DB, ...) http.HandlerFunc`, registered with method patterns (`"GET /v1/items/{item_id}"`), using `r.PathValue`.
- Error envelope: `writeError(w, status, "snake_case_code", "message")` → `{"error":{"code","message"}}`. On 5xx, `log.Printf` the real error and return a generic message.
- Store functions take `(ctx, db *sql.DB, ...)`. Wrap errors as `fmt.Errorf("doing x: %w", err)`. Use sentinel errors (`ErrItemNotFound`) checked with `errors.Is`.
- Raw SQL via `database/sql` with the pgx stdlib driver. Arrays go through `array_to_json(...)::text` or `{a,b}` literals. Vectors use the `VectorLiteral` and `ParsePgvectorText` text formats.
- Ids are `int64` internally and **strings in JSON responses**. Nullable JSON fields are pointers. Return empty slices, never `nil`, so JSON gets `[]` rather than `null`.
- Opaque cursors are base64-URL JSON.
- HTTP clients are package-level, with explicit timeouts.
- Comments explain *why*, often at length. Match that density.

### Frontend
- Server components fetch through `lib/api.ts`. Client components (`"use client"`) call server actions only.
- Expected outcomes return discriminated unions (`{kind: "ok" | "invalid"}`, `{ok: false, reason}`); only real failures throw.
- `ApiError(status, code)`: branch on `code` or `status`, never on message text. Status `0` means unreachable → render `<ApiDown>`.
- Pagination state is the API cursor alone. Filters live in the URL (`?language=`).
- Styling is plain CSS classes in `app/globals.css`, with no CSS framework. `page.module.css` is unused boilerplate.
- Use the `@/` import alias. Always pass `toLocaleString("en-US")` a locale, or hydration breaks.
- Before writing Next.js code, read `frontend/node_modules/next/dist/docs/`. Next 16 differs from older versions (`frontend/AGENTS.md`).

### Commits
Descriptive imperative subjects, sometimes prefixed with a phase (`Phase C: ...`).

## Do not
- **Don't let the browser call the Go API**, and don't expose `RECSYS_API_URL` as `NEXT_PUBLIC_*`.
- **Don't accept `user_id` from client arguments** in server actions; always read it from `auth()`.
- **Don't write events to Postgres from the API.** Publish to Kafka; the consumer owns insert and embedding recompute.
- **Don't use a bare `&kafka.Writer{}`.** Its defaults are fire-and-forget. Keep `RequireAll`, synchronous writes, and the `user_id` key.
- **Don't stamp `now()` on events in the consumer or drop `occurred_at` on retries.** That breaks dedupe.
- **Don't replace the consumer's in-place retry (`insertWithRetry`) with log-and-continue.** `kafka-go` advances past a fetched message whether or not it's committed, so the next commit would silently skip the failed event. `cmd/consumer/main_test.go` guards this.
- **Don't add dependency checks to `/healthz`.** A downstream outage would make Render restart-loop the API.
- **Don't build Kafka, Redis, or Qdrant connections by hand.** Use `events.Transport`/`events.Dialer`, `session.NewRedisClient`, and `store.SetQdrantAuth`. A connection that skips them works locally and fails only against a hosted provider.
- **Don't call `os.Getenv` or hardcode connection strings** outside `internal/config`.
- **Don't change a response shape without updating** `api/openapi.yaml` and `frontend/lib/api.ts`. Read the handler and migrations first; the spec follows the code.
- **Don't edit applied migrations.** Add a new numbered pair.
- **Don't call the LLM when no chunk clears the threshold**, and don't turn `retrieval_error` into a non-200 or merge it with `no_results` in the UI.
- **Don't reintroduce N+1 fetches.** Ranking and citations already carry title, description, stars, and URL (fixed in `e7af4a9`).
- **Don't use offset pagination for browse** or re-rank per page for recommendations.
- **Don't merge `RepoCard` and `RecommendationCard`.** They take different shapes on purpose.
- **Don't strip the auto-generated block in `frontend/AGENTS.md`**; `next dev` re-adds it.
- **Don't commit** `.env`, `frontend/.env.local`, the root `promote-qdrant` binary, or `embedding_sidecar/venv/`.
- **Don't assume test coverage.** Only `cmd/consumer` has tests. Verify changes with `go build`, `go vet`, `go test ./...`, `tsc`, `lint`, and a manual run.

## Known bugs to be aware of
- Events partitions end at 2027-12.
- The API is unauthenticated.
