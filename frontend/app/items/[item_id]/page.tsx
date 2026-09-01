import Link from "next/link";
import { notFound } from "next/navigation";
import { auth } from "@/auth";
import { getItem, ApiError } from "@/lib/api";
import { EventButtons } from "@/app/components/EventButtons";
import { ReadmeViewer } from "@/app/components/ReadmeViewer";
import { ApiDown } from "@/app/components/ApiDown";

export default async function ItemPage(props: PageProps<"/items/[item_id]">) {
  const session = await auth();
  const { item_id } = await props.params;

  let item: Awaited<ReturnType<typeof getItem>>;
  try {
    item = await getItem(item_id);
  } catch (err) {
    // A bad id is a 404/400 from the API and should render as Next's
    // not-found page; anything else is the backend being unwell.
    if (err instanceof ApiError && (err.status === 404 || err.status === 400)) {
      notFound();
    }
    return <ApiDown error={err} />;
  }

  const signedIn = Boolean(session?.recsysUserId);

  return (
    <main className="container">
      <p className="hint">
        <Link href="/">← Browse</Link>
      </p>

      <h1>
        {item.owner}/{item.name}
      </h1>
      <p className="lede">{item.description || "No description."}</p>

      <div className="meta" style={{ marginBottom: "1rem" }}>
        <span>★ {item.stars.toLocaleString("en-US")}</span>
        <span>{item.language || "unknown language"}</span>
        <a href={item.github_url} target="_blank" rel="noreferrer">
          View on GitHub ↗
        </a>
        {/* created_at is the catalog ingest time, not the repo's age on
            GitHub — labelled so it can't be misread as the latter. */}
        <span>ingested {item.created_at.slice(0, 10)}</span>
      </div>

      {item.topics.length > 0 && (
        <div className="meta" style={{ marginBottom: "1rem" }}>
          {item.topics.map((t) => (
            <span key={t} className="tag">
              {t}
            </span>
          ))}
        </div>
      )}

      <EventButtons itemID={item.item_id} signedIn={signedIn} />

      <h2 style={{ marginTop: "2rem" }}>README</h2>
      <ReadmeViewer
        itemID={item.item_id}
        sections={item.readme}
        signedIn={signedIn}
      />
    </main>
  );
}
