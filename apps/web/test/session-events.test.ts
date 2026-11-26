import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AuthenticationSession } from "@/api/generated/models";
import { getGetAuthenticationSessionQueryKey } from "@/api/generated/tacit";
import { createPresentationAbility } from "@/auth/abilities";
import { followSession } from "@/auth/session-events";

const sessionKey = getGetAuthenticationSessionQueryKey();
const firstCursor = `v1.${"A".repeat(75)}`;
const secondCursor = `v1.${"B".repeat(75)}`;
const replacementCursor = `v1.${"C".repeat(75)}`;
const userId = "usr_0123456789abcdef0123456789abcdef";
const organizationId = "org_0123456789abcdef0123456789abcdef";
const administration = `context/${organizationId}/administration`;
const initial: AuthenticationSession = {
  userId,
  organizationId,
  cursor: firstCursor,
  revision: "1",
  roles: ["admin"],
  authorization: {
    policyRevision: "tacit-authorization-v1",
    decisions: [{ action: "manage_members", resource: "administration", scope: "context", subject: administration, allowed: true }],
  },
};
const fresh: AuthenticationSession = {
  ...initial,
  cursor: replacementCursor,
  revision: "2",
  roles: ["viewer"],
  authorization: {
    ...initial.authorization,
    decisions: [{ action: "manage_members", resource: "administration", scope: "context", subject: administration, allowed: false }],
  },
};

function stream(...chunks: string[]): Response {
  const encoder = new TextEncoder();
  return new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  }), { headers: { "Content-Type": "text/event-stream" } });
}

function invalidation(resource = "authentication_session"): string {
  return `id: ${secondCursor}\nevent: invalidate\ndata: ${JSON.stringify({ resource: { type: resource, id: userId }, revision: "2" })}\n\n`;
}

function disconnected(signal: AbortSignal | null | undefined): Promise<Response> {
  return new Promise((_resolve, reject) => {
    signal?.addEventListener("abort", () => reject(new Error("closed")), { once: true });
  });
}

describe("canonical session subscription", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("retries an interrupted refetch before advancing its resume cursor and updates CASL", async () => {
    const client = new QueryClient();
    const lifetime = new AbortController();
    const timeline: string[] = [];
    let refetches = 0;
    const frame = invalidation();
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockImplementation(async (url, options) => {
      if (String(url).endsWith("/auth/session")) {
        timeline.push("canonical");
        if (++refetches === 1) return new Response(null, { status: 503 });
        return Response.json(fresh);
      }
      const cursor = new Headers(options?.headers).get("Last-Event-ID");
      timeline.push(`stream ${cursor}`);
      if (cursor === firstCursor) return stream(": connected\n\n", frame.slice(0, 20), frame.slice(20));
      return disconnected(options?.signal);
    }));
    const running = followSession({ client, initial, signal: lifetime.signal, onAccessEnded: vi.fn() });
    try {
      await vi.waitFor(() => {
        expect(timeline).toEqual([`stream ${firstCursor}`, "canonical", "canonical", `stream ${secondCursor}`]);
      });
      const current = client.getQueryData<AuthenticationSession>(sessionKey);
      expect(current?.roles).toEqual(["viewer"]);
      if (!current) throw new Error("canonical session is required");
      expect(createPresentationAbility(current.authorization).can("manage_members", administration)).toBe(false);
    } finally {
      lifetime.abort();
      await running;
      client.clear();
    }
  });

  it("purges scoped data on an unknown frame and resumes only from a fresh snapshot", async () => {
    const client = new QueryClient();
    client.setQueryData(["workspace", "cached"], "old");
    const lifetime = new AbortController();
    const scopeEnded = vi.fn();
    const cursors: (string | null)[] = [];
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockImplementation(async (url, options) => {
      if (String(url).endsWith("/auth/session")) {
        expect(client.getQueryData(sessionKey)).toBeNull();
        expect(client.getQueryData(["workspace", "cached"])).toBeUndefined();
        return Response.json(fresh);
      }
      const cursor = new Headers(options?.headers).get("Last-Event-ID");
      cursors.push(cursor);
      if (cursor === firstCursor) return stream(invalidation("unknown_resource"));
      return stream("event: access_revoked\ndata: {\"code\":\"ACCESS_REVOKED\"}\n\n");
    }));
    await followSession({ client, initial, signal: lifetime.signal, onAccessEnded: scopeEnded });
    expect(cursors).toEqual([firstCursor, replacementCursor]);
    expect(scopeEnded).toHaveBeenCalledWith("revoked");
    expect(client.getQueryData(sessionKey)).toBeNull();
    client.clear();
  });

  it("discards a late canonical read when the old workspace unmounts", async () => {
    const client = new QueryClient();
    const lifetime = new AbortController();
    const scopeEnded = vi.fn();
    const refetch = Promise.withResolvers<Response>();
    const started = Promise.withResolvers<void>();
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockImplementation(async (url) => {
      if (String(url).endsWith("/auth/session")) {
        started.resolve();
        return refetch.promise;
      }
      return stream(invalidation());
    }));
    const running = followSession({ client, initial, signal: lifetime.signal, onAccessEnded: scopeEnded });
    await started.promise;
    lifetime.abort();
    refetch.resolve(Response.json(fresh));
    await running;
    expect(client.getQueryData(sessionKey)).toEqual(initial);
    expect(scopeEnded).not.toHaveBeenCalled();
    client.clear();
  });
});
