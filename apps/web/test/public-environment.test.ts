import { describe, expect, it } from "vitest";

import { publicEnvironment } from "@/env/public";

describe("public environment", () => {
  it("uses a root-relative API base path", () => {
    expect(publicEnvironment.apiBasePath).toBe("/api");
  });
});
