// lib/api.ts
//
// Typed client for the Go recsys API. Everything here runs on the
// server only — pages, Route Handlers and Server Actions call it, the
// browser never does. That mirrors how auth.ts already reaches the API
// and means RECSYS_API_URL stays a server secret and no endpoint needs
// CORS headers.
//
// The shapes below track api/openapi.yaml. When the contract changes,
// change them here rather than casting at call sites.

const API_URL = process.env.RECSYS_API_URL ?? "http://localhost:8081";

export type Item = {
  item_id: string;
  owner: string;
  name: string;
  description: string;
  language: string;
  topics: string[];
  stars: number;
  github_url: string;
  created_at: string;
};

export type ReadmeSection = {
  chunk_index: number;
  section_heading: string;
  chunk_text: string;
};

export type ItemDetail = Item & { readme: ReadmeSection[] };

export type BrowseResponse = { items: Item[]; cursor: string | null };

export type LanguagesResponse = { languages: string[] };

export type RecommendationItem = {
  item_id: string;
  score: number;
  title: string;
  description: string;
  stars: number;
};

export type RecommendationsResponse = {
  items: RecommendationItem[];
  cursor: string | null;
  model_version: string;
  fallback: boolean;
  fallback_reason: string | null;
};

export type Citation = {
  item_id: string;
  title: string;
  repo_url: string;
  chunk_text: string;
  section_heading: string;
  chunk_index: number;
  relevance_score: number;
};

export type SearchResponse = {
  status: "grounded" | "no_results" | "retrieval_error";
  answer: string | null;
  citations: Citation[];
};

export type EventType = "viewed" | "starred" | "clicked_readme";

export type EventResponse = {
  event_id: string;
  occurred_at: string;
  status: "accepted";
};

/** ApiError carries the API's own error code so callers can branch on
 *  it (item_not_found vs. invalid_cursor) instead of on message text. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type ErrorBody = { error?: { code?: string; message?: string } };

async function request<T>(
  path: string,
  init: RequestInit & { expect?: number } = {},
): Promise<T> {
  const { expect = 200, ...rest } = init;

  let res: Response;
  try {
    // no-store throughout: recommendations are per-user and cursors are
    // single-use, so a cached response would be wrong rather than stale.
    res = await fetch(`${API_URL}${path}`, { cache: "no-store", ...rest });
  } catch {
    // Status 0 distinguishes "the API is down" from any answer it gave.
    throw new ApiError(0, "unreachable", `cannot reach the recsys API at ${API_URL}`);
  }

  if (res.status !== expect) {
    let body: ErrorBody = {};
    try {
      body = (await res.json()) as ErrorBody;
    } catch {
      // Non-JSON error body; fall through to the generic message.
    }
    throw new ApiError(
      res.status,
      body.error?.code ?? "unknown",
      body.error?.message ?? `${path} returned ${res.status}`,
    );
  }

  return (await res.json()) as T;
}

export async function listItems(params: {
  language?: string;
  cursor?: string;
  limit?: number;
}): Promise<BrowseResponse> {
  const q = new URLSearchParams();
  // The cursor already carries the language it was created under, and
  // sending both is a 400 if they ever disagree — so once we are paging,
  // the cursor alone is the filter.
  if (params.cursor) q.set("cursor", params.cursor);
  else if (params.language) q.set("language", params.language);
  if (params.limit) q.set("limit", String(params.limit));

  const qs = q.toString();
  return request<BrowseResponse>(`/v1/items${qs ? `?${qs}` : ""}`);
}

export async function getItem(itemID: string): Promise<ItemDetail> {
  return request<ItemDetail>(`/v1/items/${encodeURIComponent(itemID)}`);
}

export async function listLanguages(): Promise<string[]> {
  const body = await request<LanguagesResponse>("/v1/languages");
  return body.languages;
}

export async function getRecommendations(
  userID: number,
  cursor?: string,
): Promise<RecommendationsResponse> {
  const qs = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return request<RecommendationsResponse>(`/v1/recommendations/${userID}${qs}`);
}

export async function search(query: string): Promise<SearchResponse> {
  return request<SearchResponse>("/v1/recommendations/search", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query }),
  });
}

export async function recordEvent(input: {
  event_type: EventType;
  user_id: number;
  item_id: number;
}): Promise<EventResponse> {
  // 202, not 200: the event is queued to Kafka, not yet readable back.
  return request<EventResponse>("/v1/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
    expect: 202,
  });
}
