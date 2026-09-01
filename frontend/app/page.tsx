import { auth } from "@/auth";
import { listItems, listLanguages } from "@/lib/api";
import { BrowseList } from "./components/BrowseList";
import { LanguageFilter } from "./components/LanguageFilter";
import { ApiDown } from "./components/ApiDown";

/** Browse — the catalog, ordered by stars, optionally filtered to one
 *  language. The filter lives in the URL, so page one is always fetched
 *  fresh under the current filter and the cursor only ever pages within
 *  the filter it was born under. */
export default async function BrowsePage(props: PageProps<"/">) {
  const session = await auth();
  const { language: raw } = await props.searchParams;
  const language = typeof raw === "string" ? raw : "";

  let languages: string[];
  let page: Awaited<ReturnType<typeof listItems>>;
  try {
    [languages, page] = await Promise.all([
      listLanguages(),
      listItems({ language }),
    ]);
  } catch (err) {
    return <ApiDown error={err} />;
  }

  return (
    <main className="container">
      <h1>Browse repositories</h1>
      <p className="lede">
        The full catalog, most-starred first. Sign in to star or view a repo —
        engagement feeds the ranking behind <strong>For you</strong>.
      </p>

      <LanguageFilter languages={languages} selected={language} />

      <BrowseList
        key={language}
        initialItems={page.items}
        initialCursor={page.cursor}
        signedIn={Boolean(session?.recsysUserId)}
      />
    </main>
  );
}
