import { afterEach, describe, expect, it, vi } from "vitest";

import { localAdmission } from "@/auth/local-admission";

const session = {
  authorization: {
    decisions: [
      {
        action: "read",
        allowed: true,
        resource: "organization",
        scope: "context",
        subject: "context/org_0123456789abcdef0123456789abcdef/organization",
      },
    ],
    policyRevision: "tacit-authorization-v1",
  },
  organizationId: "org_0123456789abcdef0123456789abcdef",
  roles: ["admin"],
  userId: "usr_0123456789abcdef0123456789abcdef",
};

describe("localAdmission", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("requires a server-side access token", async () => {
    expect(await localAdmission(undefined)).toEqual({ kind: "not-admitted" });
  });

  it.each([
    [401, "not-admitted"],
    [403, "not-admitted"],
    [503, "unavailable"],
  ] as const)("maps backend status %i to %s", async (status, expected) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status })));
    expect(await localAdmission("provider-token")).toEqual({ kind: expected });
  });

  it("returns the validated local session and server decisions", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(session)));
    expect(await localAdmission("provider-token")).toEqual({ kind: "admitted", session });
  });

  it("fails closed for a malformed successful response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...session, authorization: {} })));
    expect(await localAdmission("provider-token")).toEqual({ kind: "unavailable" });
  });

  it("rejects decisions scoped to another organization", async () => {
    const crossTenant = structuredClone(session);
    const decision = crossTenant.authorization.decisions[0];
    if (decision === undefined) throw new Error("fixture decision is required");
    decision.subject = "context/org_ffffffffffffffffffffffffffffffff/organization";
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(crossTenant)));
    expect(await localAdmission("provider-token")).toEqual({ kind: "unavailable" });
  });

  it("rejects a resource subject without an exact resource identifier", async () => {
    const malformed = structuredClone(session);
    malformed.authorization.decisions = [{
      action: "read",
      allowed: true,
      resource: "meeting",
      scope: "resource",
      subject: `resource/${session.organizationId}/meeting/`,
    }];
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(malformed)));
    expect(await localAdmission("provider-token")).toEqual({ kind: "unavailable" });
  });

  it("fails closed when the backend cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    expect(await localAdmission("provider-token")).toEqual({ kind: "unavailable" });
  });
});
