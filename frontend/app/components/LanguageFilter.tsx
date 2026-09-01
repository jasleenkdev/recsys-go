"use client";

import { useRouter } from "next/navigation";

/** LanguageFilter drives the browse filter through the URL rather than
 *  local state, so a filtered listing is linkable and the server
 *  component re-fetches page one under the new filter — which is what
 *  the API wants anyway, since a cursor cannot change its language. */
export function LanguageFilter({
  languages,
  selected,
}: {
  languages: string[];
  selected: string;
}) {
  const router = useRouter();

  return (
    <div className="controls">
      <label htmlFor="language">Language</label>
      <select
        id="language"
        value={selected}
        onChange={(e) => {
          const v = e.target.value;
          router.push(v ? `/?language=${encodeURIComponent(v)}` : "/");
        }}
      >
        <option value="">All languages</option>
        {languages.map((l) => (
          <option key={l} value={l}>
            {l}
          </option>
        ))}
      </select>
    </div>
  );
}
