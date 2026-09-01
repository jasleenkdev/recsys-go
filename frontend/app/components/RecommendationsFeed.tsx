"use client";

import { useState, useTransition } from "react";
import { loadMoreRecommendations } from "@/app/actions";
import type { RecommendationItem } from "@/lib/api";
import { RecommendationCard } from "./RecommendationCard";

/** RecommendationsFeed pages through the frozen ranking snapshot the
 *  API built on the first request. Same rule as browse: the cursor is
 *  passed back verbatim and no ranking logic is repeated here. */
export function RecommendationsFeed({
  initialItems,
  initialCursor,
  modelVersion,
}: {
  initialItems: RecommendationItem[];
  initialCursor: string | null;
  modelVersion: string;
}) {
  const [items, setItems] = useState(initialItems);
  const [cursor, setCursor] = useState(initialCursor);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function more() {
    if (!cursor) return;
    setError(null);
    startTransition(async () => {
      try {
        const page = await loadMoreRecommendations(cursor);
        setItems((prev) => [...prev, ...page.items]);
        setCursor(page.cursor);
      } catch {
        setError("Could not load more recommendations.");
      }
    });
  }

  return (
    <>
      <p className="hint" style={{ marginBottom: "1rem" }}>
        Personalized ranking · model {modelVersion} · 0.8 × similarity +
        0.2 × normalized stars
      </p>
      <div className="cards">
        {items.map((item) => (
          <RecommendationCard key={item.item_id} item={item} />
        ))}
      </div>
      <div className="actions">
        {cursor ? (
          <button type="button" onClick={more} disabled={pending}>
            {pending ? "Loading…" : "Load more"}
          </button>
        ) : (
          <span className="hint">
            End of this ranking snapshot — {items.length} shown.
          </span>
        )}
        {error && <span className="hint">{error}</span>}
      </div>
    </>
  );
}
