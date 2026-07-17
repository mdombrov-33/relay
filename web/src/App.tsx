import { useCallback, useEffect, useRef, useState } from "react";
import { createRun, listRuns } from "./api";
import RunDetail from "./RunDetail";
import RunList from "./RunList";
import type { RunSummary, StoredEvent } from "./types";
import { useEventStream } from "./useEventStream";

const streamLabels = {
  connecting: "connecting",
  live: "live",
  reconnecting: "reconnecting",
} as const;

export default function App() {
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [lastEvent, setLastEvent] = useState<StoredEvent | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refreshTimer = useRef<number | undefined>(undefined);

  const refreshRuns = useCallback(async () => {
    try {
      setRuns(await listRuns());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load runs");
    }
  }, []);

  useEffect(() => {
    void refreshRuns();
    return () => window.clearTimeout(refreshTimer.current);
  }, [refreshRuns]);

  const streamState = useEventStream(
    useCallback(
      (event: StoredEvent) => {
        setLastEvent(event);
        window.clearTimeout(refreshTimer.current);
        refreshTimer.current = window.setTimeout(() => void refreshRuns(), 250);
      },
      [refreshRuns],
    ),
  );

  const handleCreate = async () => {
    try {
      const created = await createRun();
      setSelectedId(created.id);
      await refreshRuns();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create run");
    }
  };

  return (
    <div className="app">
      <header className="topbar">
        <h1 className="brand">
          Relay <span>inspector</span>
        </h1>
        <span className="stream" data-state={streamState}>
          <span className="status-dot" aria-hidden="true" />
          {streamLabels[streamState]}
        </span>
      </header>

      {error && <div className="banner">{error}</div>}

      <div className="columns">
        <aside className="sidebar">
          <div className="sidebar-head">
            <h2>Runs</h2>
            <button
              type="button"
              className="button button-primary"
              onClick={() => void handleCreate()}
            >
              New run
            </button>
          </div>
          <RunList runs={runs} selectedId={selectedId} onSelect={setSelectedId} />
        </aside>
        <main className="content">
          {selectedId ? (
            <RunDetail runId={selectedId} lastEvent={lastEvent} />
          ) : (
            <p className="empty">
              Select a run to inspect its durable status and event timeline.
            </p>
          )}
        </main>
      </div>
    </div>
  );
}
