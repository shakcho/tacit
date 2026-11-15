import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { validateDatabaseBudget } from "./database-budget.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const path = join(root, "deploy/database/connections.json");
const budget = JSON.parse(await readFile(path, "utf8"));
const totals = validateDatabaseBudget(budget);

console.log(`Database connection budget passed: ${totals.backendConnections} backend and ${totals.clientConnections} pooler clients.`);
