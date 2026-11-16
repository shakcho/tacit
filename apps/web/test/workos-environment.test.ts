import { describe, expect, it } from "vitest";

import { getCanonicalLoopbackURL, hasWorkOSConfiguration } from "@/lib/workos-environment";

const configuredEnvironment = {
  NEXT_PUBLIC_WORKOS_REDIRECT_URI: "http://127.0.0.1:3000/auth/callback",
  WORKOS_CLIENT_ID: "client_test",
  WORKOS_COOKIE_PASSWORD: "test-cookie-password-with-32-characters",
  WORKOS_ISSUER: "https://api.workos.com/",
};

describe("WorkOS environment", () => {
  it("accepts a complete configuration", () => {
    expect(hasWorkOSConfiguration(configuredEnvironment)).toBe(true);
  });

  it.each(Object.keys(configuredEnvironment))("rejects a missing %s", (missingName) => {
    const environment = Object.fromEntries(
      Object.entries(configuredEnvironment).filter(([name]) => name !== missingName),
    );

    expect(hasWorkOSConfiguration(environment)).toBe(false);
  });

  it("rejects an empty value", () => {
    expect(hasWorkOSConfiguration({ ...configuredEnvironment, WORKOS_ISSUER: "" })).toBe(false);
  });
});

describe("canonical WorkOS loopback origin", () => {
  it("moves localhost traffic to the configured loopback origin", () => {
    const canonicalURL = getCanonicalLoopbackURL({
      environment: configuredEnvironment,
      requestHost: "localhost:3000",
      requestURL: new URL("http://127.0.0.1:3000/meetings?view=recent"),
    });

    expect(canonicalURL?.toString()).toBe("http://127.0.0.1:3000/meetings?view=recent");
  });

  it("leaves the canonical origin unchanged", () => {
    expect(getCanonicalLoopbackURL({
      environment: configuredEnvironment,
      requestHost: "127.0.0.1:3000",
      requestURL: new URL("http://127.0.0.1:3000/meetings"),
    })).toBeUndefined();
  });

  it("does not redirect a public hostname", () => {
    expect(getCanonicalLoopbackURL({
      environment: configuredEnvironment,
      requestHost: "tacit.example",
      requestURL: new URL("http://127.0.0.1:3000/meetings"),
    })).toBeUndefined();
  });

  it("does not trust an invalid callback URI", () => {
    expect(getCanonicalLoopbackURL({
      environment: { ...configuredEnvironment, NEXT_PUBLIC_WORKOS_REDIRECT_URI: "not a URL" },
      requestHost: "localhost:3000",
      requestURL: new URL("http://localhost:3000/meetings"),
    })).toBeUndefined();
  });
});
