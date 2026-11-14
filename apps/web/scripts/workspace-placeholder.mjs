const task = process.argv[2];
const supported = new Set(["build", "generate", "lint", "typecheck"]);

if (!supported.has(task)) {
  console.error(`Unsupported web workspace task: ${task ?? "<missing>"}`);
  process.exitCode = 1;
} else {
  console.log(`@tacit/web ${task}: workspace ready; implementation begins in TAC-003.`);
}
