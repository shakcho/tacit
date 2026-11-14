import { readFile, readdir } from "node:fs/promises";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(await readFile(join(root, "docs/architecture/boundaries.json"), "utf8"));
const errors = [];

async function goFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await goFiles(path));
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) files.push(path);
  }
  return files;
}

for (const domainRoot of manifest.goDomainRoots) {
  const absoluteRoot = join(root, domainRoot);
  for (const file of await goFiles(absoluteRoot)) {
    const source = await readFile(file, "utf8");
    for (const fragment of manifest.forbiddenImportFragments) {
      if (source.includes(fragment)) errors.push(`${relative(root, file)} imports forbidden ${fragment}`);
    }
    const ownDomain = domainRoot.split("/").at(-1);
    const privateImport = /"github\.com\/shakcho\/tacit\/internal\/([^/]+)\/private(?:\/[^\"]*)?"/g;
    let match;
    while ((match = privateImport.exec(source))) {
      if (match[1] !== ownDomain) errors.push(`${relative(root, file)} imports ${match[1]}/private`);
    }
  }
}

if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Boundary check passed for ${manifest.goDomainRoots.length} Go domain modules on ${process.platform}${sep}.`);
}
