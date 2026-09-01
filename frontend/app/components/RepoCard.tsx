import Link from "next/link";
import type { Item } from "@/lib/api";
import { EventButtons } from "./EventButtons";

/** RepoCard is the one repo summary used by browse and by the
 *  recommendations feed, so the two cannot drift in what they show.
 *  `score` is rendered only where there is one — browse has no ranking
 *  score and should not imply otherwise. */
export function RepoCard({
  item,
  signedIn,
  score,
}: {
  item: Item;
  signedIn: boolean;
  score?: number;
}) {
  return (
    <article className="card">
      <div className="card-title">
        <Link href={`/items/${item.item_id}`}>
          {item.owner}/{item.name}
        </Link>
      </div>
      <p className="card-desc">
        {item.description || <em>No description.</em>}
      </p>
      <div className="meta">
        {/* Locale pinned: unpinned, the server formats with the host
            locale and the browser with the visitor's, which both misgroups
            the digits and trips a hydration mismatch. */}
        <span>★ {item.stars.toLocaleString("en-US")}</span>
        <span>{item.language || "unknown language"}</span>
        {score !== undefined && <span>score {score.toFixed(3)}</span>}
        {item.topics.slice(0, 4).map((t) => (
          <span key={t} className="tag">
            {t}
          </span>
        ))}
      </div>
      <EventButtons itemID={item.item_id} signedIn={signedIn} />
    </article>
  );
}
