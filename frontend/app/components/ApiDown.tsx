import { ApiError } from "@/lib/api";

/** ApiDown is the shared "the backend did not answer" state. It names
 *  the actual failure rather than rendering an empty list, so a stopped
 *  Go API never looks like an empty catalog. */
export function ApiDown({ error }: { error: unknown }) {
  const detail =
    error instanceof ApiError
      ? error.status === 0
        ? error.message
        : `${error.code}: ${error.message}`
      : "unexpected error";

  return (
    <main className="container">
      <div className="panel panel-error">
        <div className="panel-label">Backend unavailable</div>
        <p>
          The recsys API did not answer — {detail}. This page is empty because
          nothing could be loaded, not because the catalog is empty.
        </p>
      </div>
    </main>
  );
}
