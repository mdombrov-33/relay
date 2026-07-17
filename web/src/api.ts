import type { EventsPage, RunStatus, RunSummary, StoredEvent } from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      body !== null &&
      typeof body === "object" &&
      "error" in body &&
      typeof body.error === "string"
        ? body.error
        : `request failed with status ${response.status}`;
    throw new ApiError(message, response.status);
  }
  return body as T;
}

export async function listRuns(): Promise<RunSummary[]> {
  const body = await request<{ runs: RunSummary[] }>("/v1/runs");
  return body.runs;
}

export function getRun(id: string): Promise<RunSummary> {
  return request<RunSummary>(`/v1/runs/${encodeURIComponent(id)}`);
}

export function createRun(): Promise<{ id: string; status: RunStatus }> {
  return request("/v1/runs", { method: "POST" });
}

/** Pages through the bounded run-events endpoint until the durable log is drained. */
export async function listAllRunEvents(
  id: string,
): Promise<{ events: StoredEvent[]; nextAfter: number }> {
  const events: StoredEvent[] = [];
  let after = 0;
  for (;;) {
    const page = await request<EventsPage>(
      `/v1/runs/${encodeURIComponent(id)}/events?after=${after}`,
    );
    events.push(...page.events);
    if (page.events.length === 0 || page.nextAfter === after) {
      return { events, nextAfter: after };
    }
    after = page.nextAfter;
  }
}
