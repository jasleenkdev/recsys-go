"use client";

import { useState, useTransition } from "react";
import Link from "next/link";
import { runSearch, type SearchResult } from "@/app/actions";

/** SearchPanel renders the three search outcomes as three visibly
 *  different things.
 *
 *  They are all HTTP 200 and all "the server did its job", but they mean
 *  opposite things to a user: grounded is an answer, no_results is a
 *  truthful "nothing here matched", retrieval_error is "we could not
 *  look". Collapsing the last two into one grey box would tell a user
 *  the catalog lacks something when in fact the search never ran. */
export function SearchPanel() {
  const [query, setQuery] = useState("");
  const [result, setResult] = useState<SearchResult | null>(null);
  const [lastQuery, setLastQuery] = useState("");
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setFailed(false);
    setLastQuery(query);
    startTransition(async () => {
      try {
        setResult(await runSearch(query));
      } catch {
        setResult(null);
        setFailed(true);
      }
    });
  }

  return (
    <>
      <form onSubmit={submit} className="controls">
        <input
          type="search"
          value={query}
          placeholder="e.g. how do I stream logs to Kafka?"
          onChange={(e) => setQuery(e.target.value)}
          style={{ flex: 1, minWidth: "18rem" }}
        />
        <button type="submit" disabled={pending}>
          {pending ? "Searching…" : "Search"}
        </button>
      </form>

      {failed && (
        <div className="panel panel-error">
          <div className="panel-label">Request failed</div>
          <p>The search request never reached the API. Try again.</p>
        </div>
      )}

      {result?.kind === "invalid" && (
        <div className="panel panel-warn">
          <div className="panel-label">Invalid query</div>
          <p>{result.message}</p>
        </div>
      )}

      {result?.kind === "ok" && (
        <Outcome result={result} query={lastQuery} />
      )}
    </>
  );
}

function Outcome({
  result,
  query,
}: {
  result: Extract<SearchResult, { kind: "ok" }>;
  query: string;
}) {
  const { response } = result;

  if (response.status === "retrieval_error") {
    return (
      <div className="panel panel-error">
        <div className="panel-label">Retrieval error</div>
        <p>
          Search could not run — the retrieval or answer-generation backend
          was unreachable. This is <strong>not</strong> a statement about the
          catalog: nothing was searched, so nothing can be concluded about
          whether <em>{query}</em> has an answer here. Retry shortly.
        </p>
      </div>
    );
  }

  if (response.status === "no_results") {
    return (
      <div className="panel panel-warn">
        <div className="panel-label">No relevant results</div>
        <p>
          Search ran, but no README chunk cleared the relevance threshold for{" "}
          <em>{query}</em>. No answer was generated — the model is never asked
          to write one without grounding, so there is deliberately nothing to
          show rather than a plausible guess.
        </p>
      </div>
    );
  }

  return (
    <>
      <div className="panel panel-ok">
        <div className="panel-label">Grounded answer</div>
        <p className="answer">{response.answer}</p>
      </div>

      <h2>
        Citations ({response.citations.length})
      </h2>
      <p className="lede">
        Every claim above comes from these chunks. `chunk` matches the chunk
        index on the repository page, so each one can be checked in place.
      </p>
      {response.citations.map((c) => (
        <div key={`${c.item_id}-${c.chunk_index}`} className="chunk">
          <div className="chunk-head">
            <Link href={`/items/${c.item_id}`}>{c.title}</Link>
            {" · "}
            <a href={c.repo_url} target="_blank" rel="noreferrer">
              GitHub ↗
            </a>
            {" · "}
            {c.section_heading || "(preamble)"} · chunk {c.chunk_index} ·
            relevance {c.relevance_score.toFixed(3)}
          </div>
          <div className="chunk-text">{c.chunk_text}</div>
        </div>
      ))}
    </>
  );
}
