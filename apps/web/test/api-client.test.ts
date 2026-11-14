import { afterEach, describe, expect, it, vi } from "vitest";

import { apiFetch } from "@/api/client";
import type { APIError } from "@/api/client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("API client", () => {
  it("returns parsed data for a successful response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: "ok" }), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    ));

    await expect(apiFetch<{ status: string }>("/api/healthz", { method: "GET" })).resolves.toEqual({ status: "ok" });
  });

  it("throws a typed API error for a problem response", async () => {
    const problem = { code: "NOT_FOUND", status: 404, title: "Not found", type: "about:blank" };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      new Response(JSON.stringify(problem), {
        headers: {
          "Content-Type": "application/problem+json",
          "X-Request-ID": "request-42",
        },
        status: 404,
      }),
    ));

    const request = apiFetch("/api/v1/shell-samples/missing", { method: "GET" });
    await expect(request).rejects.toEqual(expect.objectContaining({
      name: "APIError",
      problem,
      requestId: "request-42",
      status: 404,
    } satisfies Partial<APIError>));
  });
});
