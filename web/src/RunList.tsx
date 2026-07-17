import StatusChip from "./StatusChip";
import type { RunSummary } from "./types";

interface Props {
  runs: RunSummary[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}

export default function RunList({ runs, selectedId, onSelect }: Props) {
  if (runs.length === 0) {
    return <p className="empty">No runs yet. Create one to start the demo.</p>;
  }

  return (
    <ul className="run-list">
      {runs.map((run) => (
        <li key={run.id}>
          <button
            type="button"
            className="run-item"
            aria-current={run.id === selectedId}
            onClick={() => onSelect(run.id)}
          >
            <span className="run-item-head">
              <code className="run-id">{run.id}</code>
              <StatusChip status={run.status} />
            </span>
            <span className="run-item-meta">
              <time dateTime={run.createdAt}>
                {new Date(run.createdAt).toLocaleString()}
              </time>
              {run.pendingApproval && (
                <span className="approval-flag">needs approval</span>
              )}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}
