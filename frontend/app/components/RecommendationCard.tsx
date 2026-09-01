import Link from "next/link";
import type { RecommendationItem } from "@/lib/api";
import { EventButtons } from "./EventButtons";

/** RecommendationCard renders a ranked item straight from the
 *  recommendations response.
 *
 *  It is deliberately not RepoCard: browse hands that component a full
 *  catalog Item, while a ranked item carries only what the ranker had
 *  loaded anyway — title, description, stars. Keeping them separate
 *  means neither component has to pretend a missing field is optional. */
export function RecommendationCard({ item }: { item: RecommendationItem }) {
  return (
    <article className="card">
      <div className="card-title">
        <Link href={`/items/${item.item_id}`}>{item.title}</Link>
      </div>
      <p className="card-desc">{item.description || <em>No description.</em>}</p>
      <div className="meta">
        {/* Locale pinned for the same reason as RepoCard: an unpinned
            format differs between server and browser and trips hydration. */}
        <span>★ {item.stars.toLocaleString("en-US")}</span>
        <span>score {item.score.toFixed(3)}</span>
      </div>
      <EventButtons itemID={item.item_id} signedIn />
    </article>
  );
}
