import { describe, expect, it } from "vitest";

import { getServerEnvironment } from "@/env/server";

describe("server environment", () => {
  it("parses the configured WorkOS organization", () => {
    const environment = getServerEnvironment({
      TACIT_WORKOS_ORGANIZATION_ID: "org_01M7FT80KK6SS1AR5WWJRY1P4Y",
    });

    expect(environment.workOSOrganizationID).toBe("org_01M7FT80KK6SS1AR5WWJRY1P4Y");
  });

  it("allows deployments to choose an organization after sign-in", () => {
    expect(getServerEnvironment({}).workOSOrganizationID).toBeUndefined();
  });

  it.each(["organization_123", "org_", "org_test-value"])(
    "rejects invalid WorkOS organization ID %s",
    (organizationID) => {
      expect(() => getServerEnvironment({ TACIT_WORKOS_ORGANIZATION_ID: organizationID })).toThrow(
        "TACIT_WORKOS_ORGANIZATION_ID must be a WorkOS organization ID",
      );
    },
  );
});
