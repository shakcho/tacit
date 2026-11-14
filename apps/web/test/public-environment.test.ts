import { describe, expect, it } from "vitest";

import { publicEnvironment } from "@/env/public";

describe("public environment", () => {
  it("uses a root-relative GraphQL path", () => {
    expect(publicEnvironment.graphQLPath.startsWith("/")).toBe(true);
  });
});
