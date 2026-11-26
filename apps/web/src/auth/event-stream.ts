import type { RealtimeCursor, SessionRevision, UserID } from "@/api/generated/models";
import { isRealtimeCursor, isRecord, isSessionRevision, isSessionUserId } from "@/auth/session-contract";

export type SessionFrame =
  | { kind: "invalidate"; cursor: RealtimeCursor; userId: UserID; revision: SessionRevision }
  | { kind: "checkpoint"; cursor: RealtimeCursor }
  | { kind: "resync" }
  | { kind: "access_revoked" };

export class StreamProtocolError extends Error {}

const maximumFrameLength = 2_048;

function parseFrame(fields: Map<string, string>): SessionFrame {
  const event = fields.get("event");
  const cursor = fields.get("id");
  let data: unknown;
  try {
    data = JSON.parse(fields.get("data") ?? "");
  } catch {
    throw new StreamProtocolError("Invalid event data");
  }
  if (isRecord(data)) {
    switch (event) {
      case "invalidate": {
        const { resource, revision } = data;
        if (isRealtimeCursor(cursor) && Object.keys(data).length === 2 &&
            isSessionRevision(revision) && revision !== "0" && isRecord(resource) &&
            Object.keys(resource).length === 2 && resource.type === "authentication_session" &&
            isSessionUserId(resource.id)) {
          return { kind: "invalidate", cursor, userId: resource.id, revision };
        }
        break;
      }
      case "checkpoint":
        if (isRealtimeCursor(cursor) && Object.keys(data).length === 0) {
          return { kind: "checkpoint", cursor };
        }
        break;
      case "resync":
        if (cursor === undefined && Object.keys(data).length === 1 && data.code === "RESYNC_REQUIRED") {
          return { kind: "resync" };
        }
        break;
      case "access_revoked":
        if (cursor === undefined && Object.keys(data).length === 1 && data.code === "ACCESS_REVOKED") {
          return { kind: "access_revoked" };
        }
        break;
    }
  }
  throw new StreamProtocolError("Invalid or unknown event");
}

export async function* readSessionFrames(
  body: ReadableStream<Uint8Array>,
  onActivity: () => void,
): AsyncGenerator<SessionFrame> {
  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  const fields = new Map<string, string>();
  let pending = "";
  let frameLength = 0;
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) return;
      onActivity();
      let text: string;
      try {
        text = decoder.decode(chunk.value, { stream: true });
      } catch {
        throw new StreamProtocolError("Invalid event encoding");
      }
      for (let offset = 0; offset < text.length;) {
        const newline = text.indexOf("\n", offset);
        const end = newline === -1 ? text.length : newline;
        frameLength += end - offset + Number(newline !== -1);
        if (frameLength > maximumFrameLength) throw new StreamProtocolError("Event too large");
        pending += text.slice(offset, end);
        if (newline === -1) break;
        offset = newline + 1;
        const line = pending.endsWith("\r") ? pending.slice(0, -1) : pending;
        pending = "";
        if (line === "") {
          if (fields.size > 0) yield parseFrame(fields);
          fields.clear();
          frameLength = 0;
        } else if (!line.startsWith(":")) {
          const colon = line.indexOf(":");
          const name = line.slice(0, colon);
          if (colon === -1 || !["event", "id", "data"].includes(name) || fields.has(name)) {
            throw new StreamProtocolError("Invalid event fields");
          }
          fields.set(name, line.slice(colon + 1).replace(/^ /u, ""));
        }
      }
    }
  } finally {
    void reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
