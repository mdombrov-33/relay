import { useEffect, useRef, useState } from "react";
import { getRun, listAllRunEvents } from "./api";
import StatusChip from "./StatusChip";
import Timeline from "./Timeline";
import type { RunSummary, StoredEvent } from "./types";

interface Props {
  runId: string;
  lastEvent: StoredEvent | null;
}

function mergeEvents(base: StoredEvent[], extra: StoredEvent[]): StoredEvent[] {
  const bySequence = new Map<number, StoredEvent>();
  for (const event of [...base, ...extra]) {
    bySequence.set(event.sequence, event);
  }
  return [...bySequence.values()].sort((a, b) => a.sequence - b.sequence);
}

export default function RunDetail({ runId, lastEvent }: Props) {
  const [run, setRun] = useState<RunSummary | null>(null);
  const [events, setEvents] = useState<StoredEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const cursor = useRef(0);

  useEffect(() => {
    let stale = false;
    setRun(null);
    setEvents([]);
    setError(null);
    cursor.current = 0;

    void (async () => {
      try {
        const [record, page] = await Promise.all([
          getRun(runId),
          listAllRunEvents(runId),
        ]);
        if (stale) {
          return;
        }
        setRun(record);
        setEvents((current) => mergeEvents(page.events, current));
        cursor.current = Math.max(cursor.current, page.nextAfter);
      } catch (err) {
        if (!stale) {
          setError(err instanceof Error ? err.message : "failed to load run");
        }
      }
    })();

    return () => {
      stale = true;
    };
  }, [runId]);

  useEffect(() => {
    if (
      !lastEvent ||
      lastEvent.runId !== runId ||
      lastEvent.sequence <= cursor.current
    ) {
      return;
    }
    cursor.current = lastEvent.sequence;
    setEvents((current) => mergeEvents(current, [lastEvent]));
    void getRun(runId)
      .then(setRun)
      .catch(() => {
        // The projection refresh retries on the next delivered event.
      });
  }, [lastEvent, runId]);

  if (error) {
    return <div className="banner">{error}</div>;
  }
  if (!run) {
    return <p className="empty">Loading run…</p>;
  }

  return (
    <div className="run-detail">
      <header className="run-detail-head">
        <div>
          <code className="run-id run-id-large">{run.id}</code>
          <p className="run-detail-meta">
            created {new Date(run.createdAt).toLocaleString()} · updated{" "}
            {new Date(run.updatedAt).toLocaleString()}
          </p>
        </div>
        <StatusChip status={run.status} />
      </header>

      {run.pendingApproval && (
        <section className="approval-panel">
          <h2>Pending approval</h2>
          <p>
            Tool <code>{run.pendingApproval.toolName}</code> at step{" "}
            <code>{run.pendingApproval.stepKey}</code> is waiting for a durable
            decision, requested{" "}
            {new Date(run.pendingApproval.requestedAt).toLocaleString()}.
          </p>
        </section>
      )}

      <section>
        <h2>Event timeline</h2>
        <Timeline events={events} />
      </section>
    </div>
  );
}
