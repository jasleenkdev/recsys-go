"use server";

// app/actions.ts
//
// The only way client components reach the Go API. Each action runs on
// the server, so RECSYS_API_URL never leaves it and — critically — the
// user_id on a write comes from the session, not from the request body.
// A client that posts its own user_id cannot log engagement as someone
// else, because no action here reads a user_id from its arguments.

import { auth } from "@/auth";
import * as api from "@/lib/api";
import type {
  Item,
  EventType,
  RecommendationItem,
  SearchResponse,
} from "@/lib/api";

export type ItemPage = { items: Item[]; cursor: string | null };

/** Fetch the next browse page. The cursor carries its own language
 *  filter, so it is the only argument the caller needs. */
export async function loadMoreItems(cursor: string): Promise<ItemPage> {
  return api.listItems({ cursor });
}

export type RecommendationPage = {
  items: RecommendationItem[];
  cursor: string | null;
  modelVersion: string;
};

/** Fetch the next recommendations page for the signed-in user.
 *
 *  Only reached after the first page, which the server component
 *  renders — so the cold-start branch is handled there, and anything
 *  arriving here already has a live snapshot behind its cursor.
 *
 *  One request per page: the response now carries the title, description
 *  and stars each card needs, so there is nothing left to look up. */
export async function loadMoreRecommendations(
  cursor: string,
): Promise<RecommendationPage> {
  const session = await auth();
  const userID = session?.recsysUserId;
  if (!userID) throw new Error("not signed in");

  const page = await api.getRecommendations(userID, cursor);
  return {
    items: page.items,
    cursor: page.cursor,
    modelVersion: page.model_version,
  };
}

export type SearchResult =
  | { kind: "ok"; response: SearchResponse }
  | { kind: "invalid"; message: string };

/** Run a grounded search. A 400 (empty query) comes back as `invalid`
 *  rather than thrown: it is user input, not a failure. The three
 *  in-band statuses are passed through untouched for the UI to render
 *  distinctly. */
export async function runSearch(query: string): Promise<SearchResult> {
  const trimmed = query.trim();
  if (!trimmed) return { kind: "invalid", message: "Enter a search query." };

  let response: SearchResponse;
  try {
    response = await api.search(trimmed);
  } catch (err) {
    if (err instanceof api.ApiError && err.status === 400) {
      return { kind: "invalid", message: err.message };
    }
    throw err;
  }

  // Citations name their own repo now, so the response is returned as
  // it arrived — one API call per search, whatever the citation count.
  return { kind: "ok", response };
}

export type EventResult =
  | { ok: true; eventType: EventType }
  | { ok: false; reason: "signed_out" | "failed"; message: string };

/** Record an engagement event for the signed-in user.
 *
 *  Returns rather than throws on the signed-out path: the UI already
 *  disables these controls when there is no session, and this is the
 *  server-side half of that same rule — no session, no event, so an
 *  event can never be attributed to a made-up user_id. */
export async function recordEvent(
  itemID: string,
  eventType: EventType,
): Promise<EventResult> {
  const session = await auth();
  const userID = session?.recsysUserId;
  if (!userID) {
    return {
      ok: false,
      reason: "signed_out",
      message: "Sign in to record this.",
    };
  }

  const numericItemID = Number(itemID);
  if (!Number.isInteger(numericItemID) || numericItemID < 1) {
    return { ok: false, reason: "failed", message: "Invalid item." };
  }

  try {
    await api.recordEvent({
      event_type: eventType,
      user_id: userID,
      item_id: numericItemID,
    });
    return { ok: true, eventType };
  } catch (err) {
    const message =
      err instanceof api.ApiError ? err.message : "Could not record event.";
    return { ok: false, reason: "failed", message };
  }
}
