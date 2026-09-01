import { auth, signIn } from "@/auth";
import { getRecommendations, listItems } from "@/lib/api";
import { RecommendationsFeed } from "@/app/components/RecommendationsFeed";
import { BrowseList } from "@/app/components/BrowseList";
import { ApiDown } from "@/app/components/ApiDown";

export default async function RecommendationsPage() {
  const session = await auth();
  const userID = session?.recsysUserId;

  if (!userID) {
    return (
      <main className="container">
        <h1>For you</h1>
        <p className="lede">
          Recommendations are per-user, so this page needs a signed-in
          identity — there is no anonymous user_id to rank against.
        </p>
        <form
          action={async () => {
            "use server";
            await signIn("github");
          }}
        >
          <button type="submit">Sign in with GitHub</button>
        </form>
      </main>
    );
  }

  let page: Awaited<ReturnType<typeof getRecommendations>>;
  try {
    page = await getRecommendations(userID);
  } catch (err) {
    return <ApiDown error={err} />;
  }

  // Cold start: the user has no embedding yet, so there is nothing to
  // personalize from. The API says so explicitly rather than inventing a
  // ranking, and this page passes that through instead of dressing the
  // popular list up as "your" recommendations.
  if (page.fallback) {
    let popular: Awaited<ReturnType<typeof listItems>>;
    try {
      popular = await listItems({});
    } catch (err) {
      return <ApiDown error={err} />;
    }

    return (
      <main className="container">
        <h1>For you</h1>
        <div className="panel panel-warn">
          <div className="panel-label">
            Not personalized · fallback ({page.fallback_reason ?? "unknown"})
          </div>
          <p>
            No personalized recommendations yet — you have no engagement
            history, so there is no embedding to rank against. Below is what
            is popular in the catalog, ordered by stars. Star or view a few
            repositories and this page becomes personalized once the consumer
            recomputes your embedding.
          </p>
        </div>
        <BrowseList
          initialItems={popular.items}
          initialCursor={popular.cursor}
          signedIn
        />
      </main>
    );
  }

  // The ranking response carries each card's title, description and
  // stars, so rendering this page is exactly one call to the Go API.
  const items = page.items;

  if (items.length === 0) {
    return (
      <main className="container">
        <h1>For you</h1>
        <p className="empty">
          The ranking returned no items. Everything in the catalog may already
          be excluded as seen.
        </p>
      </main>
    );
  }

  return (
    <main className="container">
      <h1>For you</h1>
      <p className="lede">
        Ranked from your engagement history, excluding repositories you have
        already interacted with.
      </p>
      <RecommendationsFeed
        initialItems={items}
        initialCursor={page.cursor}
        modelVersion={page.model_version}
      />
    </main>
  );
}
