const positiveIntegerFields = [
  "databaseMaxConnections",
  "providerReservedConnections",
  "poolerBackendConnections",
  "directOperationalConnections",
  "poolerMaxClients",
];

function requirePositiveInteger(value, name) {
  if (!Number.isSafeInteger(value) || value < 1) {
    throw new Error(`${name} must be a positive safe integer`);
  }
}

export function validateDatabaseBudget(budget) {
  if (budget?.schemaVersion !== 1) throw new Error("unsupported database budget schema");
  if (budget.provider !== "supabase") throw new Error("database budget provider must be supabase");

  for (const field of positiveIntegerFields) requirePositiveInteger(budget[field], field);
  if (!Array.isArray(budget.workloads) || budget.workloads.length === 0) {
    throw new Error("database budget must define workloads");
  }

  const names = new Set();
  let clientConnections = 0;
  for (const workload of budget.workloads) {
    if (typeof workload?.name !== "string" || !/^[a-z][a-z0-9-]*$/.test(workload.name)) {
      throw new Error("workload name must be a lowercase identifier");
    }
    if (names.has(workload.name)) throw new Error(`duplicate workload ${workload.name}`);
    names.add(workload.name);
    for (const field of ["maxReplicas", "rolloutSurge", "poolConnectionsPerProcess"]) {
      requirePositiveInteger(workload[field], `${workload.name}.${field}`);
    }
    const workloadConnections = (workload.maxReplicas + workload.rolloutSurge) * workload.poolConnectionsPerProcess;
    if (!Number.isSafeInteger(workloadConnections)) throw new Error(`${workload.name} connection budget overflow`);
    clientConnections += workloadConnections;
    if (!Number.isSafeInteger(clientConnections)) throw new Error("client connection budget overflow");
  }

  const backendConnections = budget.providerReservedConnections
    + budget.poolerBackendConnections
    + budget.directOperationalConnections;
  if (backendConnections >= budget.databaseMaxConnections) {
    throw new Error(`backend connection budget ${backendConnections} must stay below database capacity ${budget.databaseMaxConnections}`);
  }
  if (clientConnections >= budget.poolerMaxClients) {
    throw new Error(`client connection budget ${clientConnections} must stay below pooler capacity ${budget.poolerMaxClients}`);
  }

  return { backendConnections, clientConnections };
}
