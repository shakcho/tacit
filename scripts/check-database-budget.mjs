import { example, loadBindings, loadRegistry, projectDatabaseBudget, readJSON } from "./platform-config.mjs";

const registry = loadRegistry();
const bindings = loadBindings(readJSON(example), registry);
const totals = projectDatabaseBudget(registry, bindings.database);

console.log(`Database connection budget passed: ${totals.backendConnections} backend and ${totals.apiClients} API pooler clients.`);
