import { readFile, readdir } from "node:fs/promises";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const workspace = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = join(workspace, "src");
const operations = new Map();
const errors = [];

async function filesBelow(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await filesBelow(path));
    else if (extname(entry.name) === ".graphql") files.push(path);
  }
  return files;
}

for (const file of await filesBelow(sourceRoot)) {
  const source = await readFile(file, "utf8");
  const matches = [...source.matchAll(/\b(query|mutation|subscription)\s+([A-Z][A-Za-z0-9_]*)\b/gu)];
  if (matches.length === 0) errors.push(`${relative(workspace, file)} has no named GraphQL operation`);

  for (const match of matches) {
    const operationName = match[2];
    if (operationName === undefined) continue;
    const previous = operations.get(operationName);
    if (previous !== undefined) errors.push(`${operationName} is declared by both ${previous} and ${relative(workspace, file)}`);
    operations.set(operationName, relative(workspace, file));
  }
}

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`GraphQL document check passed for ${operations.size} named operation(s).`);
}
