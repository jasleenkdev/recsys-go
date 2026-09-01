"use client";

import { useState, useTransition } from "react";
import { recordEvent } from "@/app/actions";
import type { EventType } from "@/lib/api";

const LABELS: Record<EventType, string> = {
  starred: "Star",
  viewed: "Mark viewed",
  clicked_readme: "Opened README",
};

/** EventButtons fires POST /v1/events for the signed-in user.
 *
 *  When signed out the buttons are disabled rather than hidden, so the
 *  action stays visible and its precondition is explained. The server
 *  action refuses a signed-out call too — this is the polite half of
 *  that rule, not the enforcement. */
export function EventButtons({
  itemID,
  signedIn,
  types = ["starred", "viewed"],
}: {
  itemID: string;
  signedIn: boolean;
  types?: EventType[];
}) {
  const [pending, startTransition] = useTransition();
  const [status, setStatus] = useState<string | null>(null);

  function fire(eventType: EventType) {
    setStatus(null);
    startTransition(async () => {
      const result = await recordEvent(itemID, eventType);
      setStatus(
        result.ok
          ? // 202 means queued to Kafka, not yet stored — say so rather
            // than implying the feed has already changed.
            `${LABELS[eventType]} queued — the feed updates once the consumer recomputes.`
          : result.message,
      );
    });
  }

  return (
    <>
      <div className="actions">
        {types.map((t) => (
          <button
            key={t}
            type="button"
            disabled={!signedIn || pending}
            onClick={() => fire(t)}
          >
            {LABELS[t]}
          </button>
        ))}
        {!signedIn && <span className="hint">Sign in to record engagement.</span>}
      </div>
      {status && <p className="hint">{status}</p>}
    </>
  );
}
