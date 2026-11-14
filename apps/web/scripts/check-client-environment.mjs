import { access, readFile, readdir } from "node:fs/promises";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const workspace = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = join(workspace, "src");
const allowedPublicVariables = new Set(["NEXT_PUBLIC_API_BASE_PATH"]);
const privateVariableNames = [
  "DATABASE_URL",
  "TACIT_API_ORIGIN",
  "TACIT_AUTH_SECRET",
  "TACIT_PRIVATE_BUNDLE_CANARY",
];
const errors = [];

async function filesBelow(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await filesBelow(path));
    else files.push(path);
  }
  return files;
}

const sourceFiles = (await filesBelow(sourceRoot)).filter((file) => [".ts", ".tsx"].includes(extname(file)));

for (const file of sourceFiles) {
  const source = await readFile(file, "utf8");
  const isClientModule = /^\s*["']use client["'];/u.test(source);
  const environmentReads = source.matchAll(/process\.env\.([A-Z][A-Z0-9_]*)/gu);

  for (const match of environmentReads) {
    const variable = match[1];
    if (variable === undefined) continue;
    if (isClientModule && !variable.startsWith("NEXT_PUBLIC_")) {
      errors.push(`${relative(workspace, file)} reads private ${variable} from a client module`);
    }
    if (variable.startsWith("NEXT_PUBLIC_") && !allowedPublicVariables.has(variable)) {
      errors.push(`${relative(workspace, file)} reads undeclared public variable ${variable}`);
    }
  }

  if (isClientModule && /(?:@\/env\/server|\.\.?\/[^"']*env\/server)/u.test(source)) {
    errors.push(`${relative(workspace, file)} imports the server environment from a client module`);
  }
}

const staticRoot = join(workspace, ".next", "static");
try {
  await access(staticRoot);
  const browserChunks = await filesBelow(staticRoot);
  const privateCanary = process.env.TACIT_PRIVATE_BUNDLE_CANARY;
  const forbidden = privateCanary ? [...privateVariableNames, privateCanary] : privateVariableNames;

  for (const file of browserChunks) {
    if (![".js", ".json"].includes(extname(file))) continue;
    const source = await readFile(file, "utf8");
    for (const value of forbidden) {
      if (source.includes(value)) errors.push(`${relative(workspace, file)} contains private browser value ${value}`);
    }
  }
} catch (error) {
  if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
  console.warn("Browser bundle inspection skipped because .next/static does not exist.");
}

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Client environment boundary passed for ${sourceFiles.length} source modules.`);
}
