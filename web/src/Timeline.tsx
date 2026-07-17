import type { StoredEvent } from "./types";

export default function Timeline({ events }: { events: StoredEvent[] }) {
  if (events.length === 0) {
    return <p className="empty">No events recorded for this run yet.</p>;
  }

  return (
    <ol className="timeline">
      {events.map((event) => (
        <li key={event.sequence} className="event" data-kind={event.type.split(".")[0]}>
          <span className="event-seq">{event.sequence}</span>
          <div className="event-body">
            <div className="event-head">
              <code className="event-type">{event.type}</code>
              <code className="event-step">{event.stepKey}</code>
              <time dateTime={event.occurredAt} className="event-time">
                {new Date(event.occurredAt).toLocaleTimeString(undefined, {
                  hour12: false,
                })}
              </time>
            </div>
            <details className="event-payload">
              <summary>payload</summary>
              <pre>{JSON.stringify(event.payload, null, 2)}</pre>
            </details>
          </div>
        </li>
      ))}
    </ol>
  );
}
