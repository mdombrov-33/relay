import type { RunStatus } from "./types";

export default function StatusChip({ status }: { status: RunStatus }) {
  return (
    <span className="status-chip" data-status={status}>
      <span className="status-dot" aria-hidden="true" />
      {status}
    </span>
  );
}
