export type RunStatus =
  | "pending"
  | "running"
  | "waiting"
  | "succeeded"
  | "failed"
  | "canceled";

export interface PendingApproval {
  id: string;
  stepKey: string;
  callId: string;
  toolName: string;
  requestedAt: string;
}

export interface RunSummary {
  id: string;
  status: RunStatus;
  createdAt: string;
  updatedAt: string;
  pendingApproval?: PendingApproval;
}

export interface StoredEvent {
  sequence: number;
  id: string;
  runId: string;
  stepKey: string;
  type: string;
  occurredAt: string;
  payload: unknown;
}

export interface EventsPage {
  events: StoredEvent[];
  nextAfter: number;
}
