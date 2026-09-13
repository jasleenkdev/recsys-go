# recsys-go

recsys-go recommends GitHub repositories and answers grounded questions over their READMEs. A Go API ranks repos for each user by blending ANN similarity (Qdrant) between the user's behavioral embedding and repo embeddings with star counts. Engagement events flow through Kafka to a consumer that recomputes that embedding. A RAG endpoint answers natural-language questions from README chunks via a local LLM, and it refuses to answer when nothing relevant is retrieved. A Next.js frontend with GitHub sign-in provides browse, "For you", search, and repo detail pages. Frontend target: **Vercel**. Backend target: **Render**.

## Docs
| File | Read it for |
|---|---|
| [architecture.md](architecture.md) | **Open questions and deploy gaps**, system diagram, components, data flows, deployment topology, design decisions |
| [requirements.md](requirements.md) | Functional and non-functional requirements, full env var table, version constraints |
| [claude.md](claude.md) | Conventions, commands, deploy workflow, and the "do not" list for coding sessions |
| [api/openapi.yaml](api/openapi.yaml) | HTTP API contract |

`frontend/README.md` is `create-next-app` boilerplate and is superseded by this file.

## Quick Start (local)

**Prerequisites:** Go ≥ 1.26.5, Node ≥ 20.9 (22 recommended), Python 3.10, Postgres with pgvector, Qdrant, Redis, Kafka, Ollama. There is no docker-compose, so run each service yourself on the ports in `.env.example`.

```bash
git clone https://github.com/jasleenkdev/recsys-go.git && cd recsys-go

# 1. Backend config + schema
cp .env.example .env
migrate -path migrations -database "$DATABASE_URL" up    # assumes golang-migrate

# 2. One-time infra objects (not created by code; params assumed)
curl -X PUT localhost:6343/collections/repo_embeddings -H 'Content-Type: application/json' -d '{"vectors":{"size":768,"distance":"Cosine"}}'
curl -X PUT localhost:6343/collections/readme_chunks   -H 'Content-Type: application/json' -d '{"vectors":{"size":768,"distance":"Cosine"}}'
kafka-topics.sh --create --topic repo-events --bootstrap-server localhost:9092

# 3. Model services
(cd embedding_sidecar && python3.10 -m venv venv && . venv/bin/activate \
  && pip install fastapi uvicorn sentence-transformers && uvicorn main:app --port 8000) &
ollama pull llama3.2:3b && ollama serve &

# 4. Load data (GitHub → staging → Postgres → Qdrant)
GITHUB_TOKEN=<token> go run ./cmd/loader
go run ./cmd/promote
go run ./cmd/promote-qdrant

# 5. Run backend
go run ./cmd/api &          # :8081
go run ./cmd/consumer &

# 6. Run frontend (needs a GitHub OAuth app with callback http://localhost:3001/api/auth/callback/github)
cd frontend
cp .env.example .env.local  # fill AUTH_GITHUB_ID, AUTH_GITHUB_SECRET, AUTH_SECRET
npm ci && npm run dev       # http://localhost:3001
```

## Live Deployments
None found. The repo has no Vercel or Render config and no deployment URLs.

| Target | URL |
|---|---|
| Frontend (Vercel) | _TBD_ |
| API (Render) | _TBD_ |
| Source | https://github.com/jasleenkdev/recsys-go |
