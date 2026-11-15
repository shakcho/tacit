import { afterEach, describe, expect, it, vi } from "vitest";

import { localAdmission } from "@/auth/local-admission";

describe("localAdmission", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("requires a server-side access token", async () => {
    expect(await localAdmission(undefined)).toBe("not-admitted");
  });

  it.each([
    [200, "admitted"],
    [401, "not-admitted"],
    [403, "not-admitted"],
    [503, "unavailable"],
  ] as const)("maps backend status %i to %s", async (status, expected) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status })));
    expect(await localAdmission("provider-token")).toBe(expected);
  });

  it("fails closed when the backend cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    expect(await localAdmission("provider-token")).toBe("unavailable");
  });
});
