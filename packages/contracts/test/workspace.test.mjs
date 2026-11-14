import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("generated operation registry contains named web documents", async () => {
  const registry = await readFile(new URL("../src/generated/operations.ts", import.meta.url), "utf8");
  assert.match(registry, /ViewerShell/u);
  assert.doesNotMatch(registry, /query\s*\{/u);
});
