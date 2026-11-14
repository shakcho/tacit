import { readFile, readdir } from "node:fs/promises";
import { basename, dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const workspace = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repository = resolve(workspace, "..", "..");
const bannedPackages = [
  "@ant-design/icons",
  "antd",
  "intercom",
  "magicbell",
  "styled-components",
  "tawk",
  "tawk.to",
  "webpack",
];
const bannedConfigurationFiles = new Set([
  "webpack.config.js",
  "webpack.config.mjs",
  "webpack.config.ts",
]);
const errors = [];

async function filesBelow(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if ([".git", ".next", "node_modules"].includes(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await filesBelow(path));
    else files.push(path);
  }
  return files;
}

for (const manifestPath of [join(repository, "package.json"), join(workspace, "package.json")]) {
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  const dependencies = { ...manifest.dependencies, ...manifest.devDependencies };
  for (const packageName of bannedPackages) {
    if (packageName in dependencies) errors.push(`${relative(repository, manifestPath)} depends on banned ${packageName}`);
  }
}

for (const file of await filesBelow(workspace)) {
  if (bannedConfigurationFiles.has(basename(file))) {
    errors.push(`${relative(repository, file)} is a legacy Webpack configuration`);
  }
}

const nextConfiguration = await readFile(join(workspace, "next.config.ts"), "utf8");
if (/\bwebpack\s*[:(]/u.test(nextConfiguration)) {
  errors.push("apps/web/next.config.ts customizes Webpack instead of using the Next.js toolchain");
}

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log("Next.js toolchain check passed with no banned legacy UI packages.");
}
