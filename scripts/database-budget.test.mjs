import assert from "node:assert/strict";
import { test } from "node:test";

import { validateDatabaseBudget } from "./database-budget.mjs";

function validBudget() {
  return {
    schemaVersion: 1,
    provider: "supabase",
    databaseMaxConnections: 60,
    providerReservedConnections: 20,
    poolerBackendConnections: 15,
    directOperationalConnections: 4,
    poolerMaxClients: 200,
    workloads: [{ name: "api", maxReplicas: 3, rolloutSurge: 1, poolConnectionsPerProcess: 4 }],
  };
}

test("reports independent backend and client totals", () => {
  assert.deepEqual(validateDatabaseBudget(validBudget()), { backendConnections: 39, clientConnections: 16 });
});

test("rejects a backend budget that reaches capacity", () => {
  const budget = validBudget();
  budget.providerReservedConnections = 41;
  assert.throws(() => validateDatabaseBudget(budget), /must stay below database capacity/);
});

test("rejects a client budget that reaches pooler capacity", () => {
  const budget = validBudget();
  budget.poolerMaxClients = 16;
  assert.throws(() => validateDatabaseBudget(budget), /must stay below pooler capacity/);
});

test("rejects duplicate workload names and unsafe integers", () => {
  const budget = validBudget();
  budget.workloads.push({ ...budget.workloads[0] });
  assert.throws(() => validateDatabaseBudget(budget), /duplicate workload/);

  const overflow = validBudget();
  overflow.workloads[0].maxReplicas = Number.MAX_SAFE_INTEGER;
  assert.throws(() => validateDatabaseBudget(overflow), /connection budget overflow/);
});
