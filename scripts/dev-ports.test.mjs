import assert from "node:assert/strict";
import { test } from "node:test";

import {
  findListeningProcessIDs,
  normalizePort,
  parseProcessIDs,
  portFromAddress,
  releaseDevelopmentPorts,
} from "./dev-ports.mjs";

const quietLogger = { log() {}, warn() {} };

test("normalizes configured development ports", () => {
  assert.equal(normalizePort("03000"), "3000");
  assert.equal(portFromAddress("127.0.0.1:8080"), "8080");
  assert.equal(portFromAddress("[::]:8080"), "8080");
  assert.throws(() => normalizePort("0"), /between 1 and 65535/);
  assert.throws(() => portFromAddress("127.0.0.1"), /must end with a port/);
});

test("parses and deduplicates listener process IDs", () => {
  assert.deepEqual(parseProcessIDs("42\n84\n42\nnot-a-pid\n"), [42, 84]);
});

test("treats lsof exit status one as no listeners", () => {
  const run = () => ({ status: 1, stderr: "", stdout: "" });
  assert.deepEqual(findListeningProcessIDs([3000, 8080], run), []);
});

test("stops existing listeners gracefully", async () => {
  const states = [[42, 84], []];
  const signals = [];

  await releaseDevelopmentPorts([3000, 8080], {
    findProcessIDs: () => states.shift() ?? [],
    killProcess: (pid, signal) => signals.push([pid, signal]),
    logger: quietLogger,
    sleep: async () => {},
  });

  assert.deepEqual(signals, [[42, "SIGTERM"], [84, "SIGTERM"]]);
});

test("force-stops listeners that ignore graceful termination", async () => {
  const states = [[42], [42], [42], []];
  const signals = [];

  await releaseDevelopmentPorts([3000], {
    findProcessIDs: () => states.shift() ?? [],
    forcePeriodMs: 1,
    gracePeriodMs: 1,
    killProcess: (pid, signal) => signals.push([pid, signal]),
    logger: quietLogger,
    pollIntervalMs: 1,
    sleep: async () => {},
  });

  assert.deepEqual(signals, [[42, "SIGTERM"], [42, "SIGKILL"]]);
});
