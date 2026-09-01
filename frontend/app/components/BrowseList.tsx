"use client";

import { useState, useTransition } from "react";
import { loadMoreItems } from "@/app/actions";
import type { Item } from "@/lib/api";
import { RepoCard } from "./RepoCard";

/** BrowseList renders page one from the server and appends further
 *  pages by handing the API back the cursor it gave us. It keeps no
 *  offset, no page number and no copy of the sort key — the cursor is
 *  the whole pagination state, including the language filter it was
 *  created under. */
export function BrowseList({
  initialItems,
  initialCursor,
  signedIn,
}: {
  initialItems: Item[];
  initialCursor: string | null;
  signedIn: boolean;
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
        const page = await loadMoreItems(cursor);
        setItems((prev) => [...prev, ...page.items]);
        setCursor(page.cursor);
      } catch {
        setError("Could not load more items.");
      }
    });
  }

  if (items.length === 0) {
    return <p className="empty">No repositories match this filter.</p>;
  }

  return (
    <>
      <div className="cards">
        {items.map((item) => (
          <RepoCard key={item.item_id} item={item} signedIn={signedIn} />
        ))}
      </div>
      <div className="actions">
        {cursor ? (
          <button type="button" onClick={more} disabled={pending}>
            {pending ? "Loading…" : "Load more"}
          </button>
        ) : (
          <span className="hint">End of catalog — {items.length} shown.</span>
        )}
        {error && <span className="hint">{error}</span>}
      </div>
    </>
  );
}
