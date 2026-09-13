# Requirements — recsys-go

Inferred from what is implemented at commit `e7af4a9`. The API contract lives in [`api/openapi.yaml`](api/openapi.yaml), and the deploy gaps are listed in [architecture.md → Open Questions](architecture.md#open-questions--gaps).

---

## Functional Requirements

### Identity
- **FR-1** Users sign in with GitHub (Auth.js v5, JWT sessions).
- **FR-2** Each GitHub account maps to exactly one recsys `user_id` via an idempotent upsert on `external_id = "github:<numeric id>"`. If sync fails, sign-in fails.
- **FR-3** Browse, item detail, and search work signed out. Recommendations and event recording require sign-in.

### Catalog
- **FR-4** Browse all repos ordered by stars desc (id desc as tie-break), with an optional exact-match language filter.
- **FR-5** Paginate browse with an opaque keyset cursor, `limit` 1–100 (default 20). A malformed cursor, or a language that contradicts the cursor, returns 400.
- **FR-6** List the languages present in the catalog, ordered by repo count.
- **FR-7** Item detail returns metadata, a derived `github_url`, and README chunks in order. A missing README is an empty array, not an error.

### Engagement
- **FR-8** Record `viewed`, `starred`, and `clicked_readme` events. `clicked_readme` fires once, when the README is first expanded.
- **FR-9** Events are accepted with 202 and processed asynchronously. Unknown `user_id` or `item_id` returns 404; a queue failure returns 503.
- **FR-10** Retrying with the same `event_id` **and** `occurred_at` stores the event exactly once.
- **FR-11** After each stored event, the user's embedding is recomputed as a weighted average of engaged item embeddings (starred 3, clicked_readme 2, viewed 1).

### Recommendations
- **FR-12** Rank candidates as `0.8 × ANN cosine similarity + 0.2 × stars normalized to the pool max`, excluding items the user already has events for.
- **FR-13** Freeze up to 100 ranked items per session for 10 minutes and serve fixed pages of 20. An invalid or expired cursor silently starts a new snapshot.
- **FR-14** A user with no embedding gets `fallback: true, fallback_reason: "cold_start"`, and the UI shows the popular catalog labelled as non-personalized.

### Grounded Search
- **FR-15** Natural-language query → embed → top 5 README chunks → keep chunks with score ≥ 0.5.
- **FR-16** If no chunk clears the threshold, return `no_results` without calling the LLM.
- **FR-17** Otherwise, generate an answer from those chunks only (`llama3.2:3b`) and return citations with repo title, URL, section, `chunk_index`, and score.
- **FR-18** A dependency failure returns HTTP 200 with `status: retrieval_error`. The UI renders the three statuses distinctly.

### Ingestion (offline CLIs)
- **FR-19** `loader` stages up to 150 repos for each of 4 categories (Go/backend, Python/ML, JS/frontend, Rust/CLI) plus READMEs chunked on `## ` headings. Re-runs skip repos already staged.
- **FR-20** `promote` embeds staged repos (`name: description`) and chunks into Postgres. `promote-qdrant` pushes all vectors to Qdrant.

### Operations
- **FR-21** `GET /healthz` (unversioned) returns `200 {"status":"ok"}` whenever the API process serves HTTP. It is used as the Render health check and never checks downstream dependencies.
- **FR-22** The API listens on `PORT` when set (Render), otherwise `API_PORT`, otherwise `8081`.

---

## Non-Functional Requirements

### Performance knobs (hardcoded constants)
| Knob | Value | Location |
|---|---|---|
| Recs page size / pool / ANN over-fetch | 20 / 100 / 150 | `cmd/api/main.go`, `store/candidates.go` |
| Rec session TTL | 10 min | `session/session.go` |
| Browse limit | default 20, max 100 | `cmd/api/main.go`, `store/items.go` |
| Search chunks / threshold | 5 / 0.5 (marked provisional) | `store/search.go` |
| HTTP timeouts | Qdrant and sidecar 10s, Ollama 60s, Kafka write 5s, loader 15s, promote 30s | various |
| Frontend fetch | `cache: "no-store"` everywhere, no timeout | `lib/api.ts` |

### Reliability
- Event writes are idempotent. Permanent Postgres errors (`22P02`, `23502`, `23503`) are skipped and committed.
- A transient insert failure is retried in place with backoff (200ms doubling to a 30s cap) before the next message is fetched, so a later commit can't skip it. Embedding recompute failures are still only logged.
- **Gap:** `http.ListenAndServe` has no server timeouts and no graceful shutdown; the DB pool has no limits; there is no startup ping.
- **Gap:** events partitions exist only through 2027-12.

### Security
- **No authentication on the Go API**, no rate limiting, and no CORS (none needed, since only the Next.js server calls it). If the API is public, anyone can forge events.
- The frontend never takes `user_id` from client input; it always reads it from the session (`app/actions.ts`).
- Redis, Kafka, and Qdrant clients support optional auth and TLS via env vars. It's off when unset, and half-set auth (e.g. a SASL mechanism without a password) is rejected at startup without logging secret values.
- Secrets: `.env` and `frontend/.env.local` are gitignored; only `.env.example` files are tracked.

### Scalability
- The API is stateless (session state lives in Redis), so it can scale horizontally.
- The consumer scales up to the `repo-events` partition count. Per-user ordering depends on the `user_id` key.
- The LLM call (up to 60s) and CPU embedding dominate search latency.

### Observability
- `log.Printf` only. No metrics, tracing, or request logging.
- `GET /healthz` is a liveness-only probe: always `200 {"status":"ok"}` while the process serves HTTP, with no dependency checks.

### Render tier constraints (verify against current Render pricing)
| Tier | Relevance |
|---|---|
| Free web service | Spins down after ~15 min idle, so the first request after idle takes tens of seconds; ~512 MB RAM. Fine for the API binary, **not** for the sidecar or Ollama. |
| Background workers | Paid only, and needed for `cmd/consumer`. |
| Free Postgres | Time-limited expiry and small storage. Confirm pgvector is available. |
| Key Value (Redis) | The internal address works with no auth vars. For an external or authenticated instance, set `REDIS_PASSWORD` (and `REDIS_TLS=true` if required). |

---

## Environment & Config

Go binaries read config only through `internal/config`, which loads `.env` from the **current working directory**. `cmd/loader` is the one exception: it reads `GITHUB_TOKEN` directly. Empty values fall back to the default.

| Variable | Purpose | Code default | Used by | Local | Vercel | Render |
|---|---|---|---|---|---|---|
| `DATABASE_URL` | Postgres DSN | `postgres://jasleenkaur@localhost:5432/recsys?sslmode=disable` | api, consumer, loader, promote, promote-qdrant, test-candidates | `.env` | — | api, consumer, jobs |
| `QDRANT_URL` | Qdrant base URL, no trailing slash; use `https://` for TLS | `http://localhost:6343` | api, promote-qdrant | `.env` | — | api, jobs |
| `QDRANT_API_KEY` | **Secret.** Qdrant Cloud API key, sent as the `api-key` header | unset (no header) | api, promote-qdrant | unset | — | api, jobs (Qdrant Cloud) |
| `REDIS_ADDR` | Redis `host:port` for rec sessions | `localhost:6390` | api | `.env` | — | api |
| `REDIS_USERNAME` | Redis ACL username; requires `REDIS_PASSWORD` | unset | api | unset | — | api (only if the provider uses ACL users) |
| `REDIS_PASSWORD` | **Secret.** Redis password; alone, it authenticates as the default user | unset (no AUTH) | api | unset | — | api (hosted Redis) |
| `REDIS_TLS` | `true` enables TLS to Redis | `false` | api | unset | — | api (if the provider requires TLS) |
| `KAFKA_BROKERS` | Comma-separated brokers | `localhost:9092` | api, consumer, producer-test | `.env` | — | api, consumer |
| `KAFKA_TLS` | `true` enables TLS to the brokers | `false` | api, consumer | unset | — | api, consumer (hosted Kafka) |
| `KAFKA_SASL_MECHANISM` | `PLAIN`, `SCRAM-SHA-256` or `SCRAM-SHA-512` (case-insensitive); unset means no SASL | unset | api, consumer | unset | — | api, consumer (hosted Kafka) |
| `KAFKA_SASL_USERNAME` | SASL username; required with a mechanism | unset | api, consumer | unset | — | api, consumer (hosted Kafka) |
| `KAFKA_SASL_PASSWORD` | **Secret.** SASL password; required with a mechanism | unset | api, consumer | unset | — | api, consumer (hosted Kafka) |
| `EMBED_SIDECAR_URL` | Full sidecar `/embed` URL | `http://localhost:8000/embed` | api, promote | `.env` | — | api, jobs |
| `OLLAMA_URL` | Full Ollama `/api/generate` URL | `http://localhost:11434/api/generate` | api | `.env` | — | api |
| `PORT` | API listen port; takes precedence over `API_PORT` | unset | api | normally unset | — | **injected by Render**; don't set it manually |
| `API_PORT` | API listen port for local dev, used only when `PORT` is unset | `8081` | api | `.env` | — | leave unset |
| `GITHUB_TOKEN` | GitHub REST token; loader exits without it | none | loader | shell (not in `.env.example`) | — | jobs |
| `AUTH_GITHUB_ID` | GitHub OAuth client id | — | frontend | `frontend/.env.local` | ✅ | — |
| `AUTH_GITHUB_SECRET` | GitHub OAuth client secret | — | frontend | `frontend/.env.local` | ✅ | — |
| `AUTH_SECRET` | Auth.js JWT signing (`openssl rand -base64 32`) | — | frontend | `frontend/.env.local` | ✅ | — |
| `RECSYS_API_URL` | Go API base URL, **server-only** | `http://localhost:8081` | frontend (`auth.ts`, `lib/api.ts`) | `frontend/.env.local` | ✅ Render URL | — |

All auth variables are optional and off when unset. Half-set auth (a mechanism without credentials, credentials without a mechanism, `REDIS_USERNAME` without a password, or a non-boolean `*_TLS`) is fatal at startup, and error messages name variables, never values. `cmd/producer-test`, a local smoke script, does not apply Kafka auth.

Not used by the code, but may be needed: `AUTH_URL` or `AUTH_TRUST_HOST`. Auth.js infers the host on Vercel; set one explicitly if you host the frontend elsewhere.

Hardcoded values, not configurable: Kafka topic `repo-events`, consumer group `events-consumer`, Ollama model `llama3.2:3b`, sidecar model `all-mpnet-base-v2`, Qdrant collection names.

### Local ports
| Service | Port |
|---|---|
| Frontend (`next dev`) | 3001 |
| API | 8081 |
| Postgres | 5432 |
| Qdrant | 6343 (non-default; avoids a local conflict) |
| Redis | 6390 |
| Kafka | 9092 |
| Sidecar | 8000 |
| Ollama | 11434 |

---

## Version Constraints

| Component | Constraint | Why it matters for deploy |
|---|---|---|
| Go | `go 1.26.5` in `go.mod` | The build image needs ≥1.26.5 or `GOTOOLCHAIN=auto`; the code uses method-pattern `ServeMux` (≥1.22) |
| pgx | v5.10.0 | Uses the `database/sql` stdlib driver (`"pgx"`) |
| kafka-go | v0.4.51 | TLS, SASL/PLAIN, SCRAM-SHA-256/512 via `sasl/plain` and `sasl/scram`; SCRAM pulls in `xdg-go/scram` v1.1.2. AWS MSK IAM isn't wired up. |
| go-redis | v9.22.0 | — |
| Node.js | ≥ 20.9 (Next 16 engine); 22.22 used locally | Set Vercel Node to 22.x |
| Next.js | 16.3.2 (exact) | Breaking changes vs. older Next; see `frontend/AGENTS.md` |
| next-auth | `^5.0.0-beta.32` (beta) | A caret on a beta can pull breaking betas; `package-lock.json` pins beta.32 |
| React | 19.2.8 | — |
| Postgres | `vector` extension required (`vector(768)`), plus `gen_random_uuid()` (PG ≥13) | Host must allow `CREATE EXTENSION vector` |
| Python sidecar | 3.10.13 venv: fastapi 0.141.1, uvicorn 0.52.3, sentence-transformers 5.7.0, torch 2.13.0, transformers 5.15.0 | **No requirements.txt**; pin these before hosting |
| Embedding dim | 768 everywhere (schema, models seed, Qdrant) | Changing the model means new migrations and re-ingest |
