# recsys-go

A GitHub repository recommendation microservice combining **behavioral ANN
candidate generation** with a **grounded RAG search** endpoint, built to
demonstrate real systems design: contract-first API design, a
versioned schema with database-enforced integrity, idempotent event
ingestion, and honest failure/fallback states throughout.

Live on 600 real GitHub repositories across four categories (Go/backend,
Python/ML, JavaScript/frontend, Rust/CLI), with 4,690 embedded README
chunks.

## What it does

- **Personalized recommendations** — ranks repos for a signed-in user by
  blending ANN similarity (a weighted average of the embeddings of repos
  they've engaged with) with normalized star count, excluding anything
  they've already seen.
- **Grounded search** — natural-language queries against README content.
  Answers are generated *only* from retrieved chunks that clear a
  relevance threshold; if nothing is relevant enough, the system says so
  instead of guessing.
- **Live personalization** — starring or viewing a repo fires an event
  through Kafka, which recomputes that user's embedding, so the next page
  load reflects the new signal (eventually consistent, not instant).
- **Cold-start handling** — a brand-new user with no history gets an
  explicit, labeled fallback (popular repos), never a confusing empty page
  or a silently-guessed personalization.

## Architecture

```
                    ┌─────────────┐
   GitHub API  ───▶ │   loader    │──▶ staging tables (Postgres)
                    └─────────────┘
                           │
                    ┌─────────────┐      ┌──────────────┐
                    │   promote   │─────▶│  embedding   │
                    │ (sweep 1)   │◀─────│   sidecar    │
                    └─────────────┘      │ (FastAPI +   │
                           │             │ sentence-    │
                    ┌─────────────┐      │ transformers)│
                    │promote-qdrant      └──────────────┘
                    │ (sweep 2)   │
                    └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │   Qdrant    │  (repo_embeddings, readme_chunks)
                    └─────────────┘

  Frontend (Next.js)          Kafka              Postgres (pgvector)
        │                       ▲                       ▲
        ▼                       │                       │
  ┌─────────────┐        ┌─────────────┐         │
  │  Go API      │──────▶ │  producer   │         │
  │ (cmd/api)   │        └─────────────┘         │
  └─────────────┘                                       │
        │              ┌─────────────┐                  │
        ├─────────────▶│  consumer   │──────────────────┘
        │              │ (idempotent,│  recomputes user embedding
        │              │ recomputes  │  on every processed event
        │              │ embeddings) │
        ▼              └─────────────┘
  ┌─────────────┐
  │    Redis    │  (snapshot-cursor pagination)
  └─────────────┘
        │
        ▼
  ┌─────────────┐
  │   Ollama    │  (local LLM, grounded answer generation)
  └─────────────┘
```

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Backend | Go | The service itself — API, ingestion, ranking |
| Database | PostgreSQL + pgvector | Source of truth for everything, including vectors |
| Vector search | Qdrant | Derived, rebuildable ANN index over Postgres |
| Streaming | Kafka | Event ingestion, partitioned by user for ordering |
| Cache/sessions | Redis | Snapshot-cursor pagination |
| Embeddings | `all-mpnet-base-v2` (sentence-transformers) | Free, local, pretrained — no training data or infra needed at this scale |
| Search LLM | Ollama (`llama3.2:3b`), local | Free, local; Gemini API planned for the hosted deployment |
| Frontend | Next.js + NextAuth (GitHub OAuth) | Public-facing UI, real identity |

## API

All endpoints are documented in [`api/openapi.yaml`](./api/openapi.yaml),
kept in sync with the actual server behavior.

| Endpoint | Description |
|---|---|
| `GET /v1/recommendations/{user_id}` | Cursor-paginated, ranked recommendations |
| `POST /v1/recommendations/search` | Grounded RAG search over READMEs |
| `POST /v1/events` | Record a user interaction (viewed/starred/clicked_readme) |
| `GET /v1/items` | Browse the catalog, filterable by language |
| `GET /v1/items/{item_id}` | Single repo detail |
| `GET /v1/languages` | Available filter values |
| `POST /v1/auth/sync` | Idempotent user upsert, called by the frontend's NextAuth callback |

## Project structure

```
cmd/
  api/              HTTP server
  consumer/         Kafka consumer — idempotent inserts, live embedding recompute
  loader/           Fetches repos + READMEs from the GitHub API into staging
  promote/          Sweep 1: staging → Postgres (with embeddings)
  promote-qdrant/   Sweep 2: Postgres → Qdrant
  producer-test/    Manual test event producer
  test-candidates/  Manual ANN candidate test script
internal/
  config/           Environment-variable-driven configuration
  domain/           Core types (RepoEvent, EventType)
  session/          Redis-backed cursor pagination
  store/            All business logic: candidates, search, embeddings, events
embedding_sidecar/  Python/FastAPI wrapper around sentence-transformers
frontend/           Next.js app (browse, recommendations, search, auth)
migrations/         Versioned schema (golang-migrate)
api/openapi.yaml    API contract
```

## Running locally

Requires: Go, Node.js, Python 3.10+, Docker, PostgreSQL (native), Ollama.

1. **Start infrastructure:**
   ```bash
   docker start redis qdrant kafka   # or `docker run` if containers don't exist yet
   ```
2. **Apply migrations:**
   ```bash
   migrate -database "$DATABASE_URL" -path migrations up
   ```
3. **Start the embedding sidecar:**
   ```bash
   cd embedding_sidecar && source venv/bin/activate && uvicorn main:app --port 8000
   ```
4. **Start Ollama:**
   ```bash
   ollama serve   # separate terminal, if not already running
   ```
5. **Start the Go services** (each in its own terminal):
   ```bash
   go run cmd/api/main.go
   go run cmd/consumer/main.go
   ```
6. **Start the frontend:**
   ```bash
   cd frontend && npm run dev
   ```

Configuration is environment-variable driven — see `.env.example` (root)
and `frontend/.env.example` for what's needed. Sensible localhost defaults
are built in, so most of this works out of the box for local development.

### Loading real data (one-time)

```bash
go run cmd/loader/main.go          # fetch repos + READMEs from GitHub
go run cmd/promote/main.go         # embed + promote to Postgres
go run cmd/promote-qdrant/main.go  # push vectors to Qdrant
```


## Status

Core recommendation engine, event pipeline, RAG search, GitHub OAuth,
and a full frontend covering every backend endpoint are built and
verified against real data. Public deployment is the remaining phase.
