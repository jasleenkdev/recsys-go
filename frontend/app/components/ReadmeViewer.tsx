"use client";

import { useState, useTransition } from "react";
import { recordEvent } from "@/app/actions";
import type { ReadmeSection } from "@/lib/api";

/** ReadmeViewer shows the README as the same chunks the RAG path cites,
 *  labelled with the chunk_index a search citation carries — so a
 *  citation can be located in the source document by eye.
 *
 *  Opening it fires clicked_readme once: the event describes an actual
 *  read, so it is tied to the expand rather than to page load. */
export function ReadmeViewer({
  itemID,
  sections,
  signedIn,
}: {
  itemID: string;
  sections: ReadmeSection[];
  signedIn: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [fired, setFired] = useState(false);
  const [, startTransition] = useTransition();

  if (sections.length === 0) {
    return <p className="empty">No README was ingested for this repository.</p>;
  }

  function toggle() {
    const next = !open;
    setOpen(next);
    if (next && !fired && signedIn) {
      setFired(true);
      startTransition(async () => {
        await recordEvent(itemID, "clicked_readme");
      });
    }
  }

  return (
    <>
      <div className="actions">
        <button type="button" onClick={toggle}>
          {open ? "Hide" : "Show"} README ({sections.length} chunks)
        </button>
        {!signedIn && (
          <span className="hint">
            Sign in to record this as a clicked_readme event.
          </span>
        )}
      </div>
      {open && (
        <div style={{ marginTop: "1rem" }}>
          {sections.map((s) => (
            <div key={s.chunk_index} className="chunk">
              <div className="chunk-head">
                chunk {s.chunk_index}
                {s.section_heading ? ` · ${s.section_heading}` : " · (preamble)"}
              </div>
              <div className="chunk-text">{s.chunk_text}</div>
            </div>
          ))}
        </div>
      )}
    </>
  );
}
