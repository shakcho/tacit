import { NextRequest } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import { GET } from "@/app/api/[...path]/route";

vi.mock("@workos-inc/authkit-nextjs", () => ({
  withAuth: async () => ({ user: { id: "provider-user" }, accessToken: "server-token" }),
}));

const context = { params: Promise.resolve({ path: ["v1", "events"] }) };

describe("session event proxy", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("passes frames incrementally and keeps upstream open past the connection deadline", async () => {
    vi.useFakeTimers();
    const encoder = new TextEncoder();
    const cancelled = vi.fn();
    let enqueue: (frame: string) => void = () => { throw new Error("upstream is not open"); };
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        enqueue = (frame) => { controller.enqueue(encoder.encode(frame)); };
        enqueue(": connected\n\n");
      },
      cancel: cancelled,
    });
    const fetchUpstream = vi.fn<typeof fetch>().mockResolvedValue(new Response(body, {
      headers: {
        "Cache-Control": "no-store, no-transform",
        "Content-Type": "text/event-stream",
        "Set-Cookie": "private=cookie",
        "X-Accel-Buffering": "no",
      },
    }));
    vi.stubGlobal("fetch", fetchUpstream);
    const browser = new AbortController();
    const request = new NextRequest("http://127.0.0.1:3000/api/v1/events", {
      headers: { Cookie: "browser-cookie", "Last-Event-ID": "last-cursor" },
      signal: browser.signal,
    });
    const response = await GET(request, context);
    const forwarded = fetchUpstream.mock.calls[0]?.[1];
    expect(new Headers(forwarded?.headers).get("Authorization")).toBe("Bearer server-token");
    expect(new Headers(forwarded?.headers).get("Cookie")).toBeNull();
    expect(new Headers(forwarded?.headers).get("Accept")).toBe("text/event-stream");
    expect(new Headers(forwarded?.headers).get("Last-Event-ID")).toBe("last-cursor");
    expect(response.headers.get("Cache-Control")).toBe("no-store, no-transform");
    expect(response.headers.get("X-Accel-Buffering")).toBe("no");
    expect(response.headers.has("Set-Cookie")).toBe(false);
    if (response.body === null) throw new Error("stream body is required");
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    expect(decoder.decode((await reader.read()).value)).toBe(": connected\n\n");

    await vi.advanceTimersByTimeAsync(25_000);
    expect(forwarded?.signal?.aborted).toBe(false);
    enqueue(": heartbeat\n\n");
    expect(decoder.decode((await reader.read()).value)).toBe(": heartbeat\n\n");
    browser.abort();
    expect(forwarded?.signal?.aborted).toBe(true);
    await reader.cancel();
    expect(cancelled).toHaveBeenCalledOnce();
  });

  it("bounds upstream connection establishment", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockImplementation((_url, options) => new Promise((_resolve, reject) => {
      options?.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    })));
    const pending = GET(new NextRequest("http://127.0.0.1:3000/api/v1/events"), context);
    await vi.advanceTimersByTimeAsync(15_001);
    expect((await pending).status).toBe(503);
  });

  it("rejects cross-site and caller-selected scope before forwarding", async () => {
    const fetchUpstream = vi.fn();
    vi.stubGlobal("fetch", fetchUpstream);
    const endpoint = "http://127.0.0.1:3000/api/v1/events";
    const crossSite = new NextRequest(endpoint, { headers: { "Sec-Fetch-Site": "cross-site" } });
    const rawScope = new NextRequest(`${endpoint}?organizationId=another`);
    expect((await GET(crossSite, context)).status).toBe(403);
    expect((await GET(rawScope, context)).status).toBe(400);
    expect(fetchUpstream).not.toHaveBeenCalled();
  });
});
