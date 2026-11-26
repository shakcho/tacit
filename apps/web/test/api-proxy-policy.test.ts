import { describe, expect, it } from "vitest";

import {
  backendPath,
  downstreamResponseHeaders,
  hasExpectedOrigin,
  isAllowedBackendRequest,
  upstreamRequestHeaders,
} from "@/auth/api-proxy-policy";

describe("API proxy policy", () => {
  it("accepts only explicit backend operations", () => {
    expect(isAllowedBackendRequest("GET", "/v1/auth/session")).toBe(true);
    expect(isAllowedBackendRequest("GET", "/v1/events")).toBe(true);
    expect(isAllowedBackendRequest("GET", "/v1/events\n")).toBe(false);
    expect(isAllowedBackendRequest("HEAD", "/v1/events")).toBe(false);
    expect(isAllowedBackendRequest("POST", "/v1/shell-samples")).toBe(true);
    expect(isAllowedBackendRequest("GET", "/internal/auth/session-denials")).toBe(false);
    expect(isAllowedBackendRequest("POST", "/webhooks/workos")).toBe(false);
  });

  it("rejects ambiguous path segments", () => {
    expect(backendPath(["v1", "auth", "session"])).toBe("/v1/auth/session");
    expect(backendPath(["v1", "..", "readyz"])).toBeNull();
    expect(backendPath(["v1/auth", "session"])).toBeNull();
  });

  it("requires the configured origin for unsafe requests", () => {
    const allowed = new Request("http://127.0.0.1:3000/api/v1/shell-samples", {
      headers: { Origin: "http://127.0.0.1:3000", "Sec-Fetch-Site": "same-origin" },
      method: "POST",
    });
    const denied = new Request("http://127.0.0.1:3000/api/v1/shell-samples", {
      headers: { Origin: "https://attacker.example" },
      method: "POST",
    });
    expect(hasExpectedOrigin(allowed, "http://127.0.0.1:3000")).toBe(true);
    expect(hasExpectedOrigin(denied, "http://127.0.0.1:3000")).toBe(false);
  });

  it("replaces credentials and filters response headers", () => {
    const request = new Request("http://127.0.0.1:3000/api/v1/auth/session", {
      headers: { Cookie: "secret", "X-Request-ID": "request-1", "X-WorkOS-Session": "secret" },
    });
    const upstream = upstreamRequestHeaders(request, "server-token");
    expect(upstream.get("Authorization")).toBe("Bearer server-token");
    expect(upstream.has("Cookie")).toBe(false);
    expect(upstream.has("X-WorkOS-Session")).toBe(false);

    const downstream = downstreamResponseHeaders(new Headers({
      "Content-Type": "application/json",
      "Set-Cookie": "backend=secret",
      "X-Request-ID": "request-1",
    }));
    expect(downstream.get("Cache-Control")).toBe("no-store");
    expect(downstream.has("Set-Cookie")).toBe(false);
  });
});
