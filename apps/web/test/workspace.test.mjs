import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("web workspace remains private and ESM", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.equal(manifest.private, true);
  assert.equal(manifest.type, "module");
});
