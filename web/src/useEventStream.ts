import { useEffect, useRef, useState } from "react";
import type { StoredEvent } from "./types";

export type StreamState = "connecting" | "live" | "reconnecting";

/**
 * Follows the global durable event stream. The server resumes strictly after
 * the supplied cursor and ignores Last-Event-ID, so reconnection recreates the
 * EventSource with the last fully received sequence instead of relying on the
 * browser's automatic retry.
 */
export function useEventStream(onEvent: (event: StoredEvent) => void): StreamState {
  const [state, setState] = useState<StreamState>("connecting");
  const handler = useRef(onEvent);
  handler.current = onEvent;

  useEffect(() => {
    let source: EventSource | null = null;
    let retry: number | undefined;
    let after = 0;
    let closed = false;

    const connect = () => {
      source = new EventSource(`/v1/events/stream?after=${after}`);
      source.onopen = () => setState("live");
      source.onmessage = (message) => {
        const event = JSON.parse(message.data) as StoredEvent;
        after = event.sequence;
        handler.current(event);
      };
      source.onerror = () => {
        source?.close();
        if (closed) {
          return;
        }
        setState("reconnecting");
        retry = window.setTimeout(connect, 1500);
      };
    };
    connect();

    return () => {
      closed = true;
      window.clearTimeout(retry);
      source?.close();
    };
  }, []);

  return state;
}
