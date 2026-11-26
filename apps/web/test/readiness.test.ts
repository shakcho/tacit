import { afterEach, describe, expect, it, vi } from "vitest";

import { GET as health } from "@/app/healthz/route";
import { GET as readiness } from "@/app/readyz/route";

describe("web probes", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  it("keeps liveness local when production identity is missing", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("WORKOS_CLIENT_ID", "");
    expect(health().status).toBe(200);
    expect((await readiness()).status).toBe(503);
  });

  it("accepts keyless production identity and requires upstream admission", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("TACIT_WEB_ORIGIN", "https://tacit.example");
    vi.stubEnv("TACIT_API_ORIGIN", "http://api.example:8080");
    vi.stubEnv("WORKOS_CLIENT_ID", "client_example");
    vi.stubEnv("WORKOS_API_KEY", "");
    vi.stubEnv("WORKOS_COOKIE_PASSWORD", "x".repeat(32));
    vi.stubEnv("WORKOS_ISSUER", "https://workos.example");
    vi.stubEnv("NEXT_PUBLIC_WORKOS_REDIRECT_URI", "https://tacit.example/auth/callback");
    const fetch = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockResolvedValueOnce(new Response(null, { status: 200 }));
    expect((await readiness()).status).toBe(503);
    expect((await readiness()).status).toBe(200);
    expect(fetch).toHaveBeenCalledWith(new URL("http://api.example:8080/readyz"), expect.objectContaining({
      cache: "no-store",
      redirect: "error",
    }));
  });
});
