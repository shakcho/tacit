import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import {
  assertSupportedSchema, example, loadBindings, loadRegistry, projectDatabaseBudget, readJSON, stableJSON,
} from "./platform-config.mjs";
import {
  changeRelease, commandFor, parseCLI, parseManifests, requireCompatibleRevision, withEnvironmentLock,
} from "./platform.mjs";

const registry = loadRegistry();
const bindings = loadBindings(readJSON(example), registry);

test("the registry is closed, versioned, and makes contracts non-runnable", () => {
  assert.throws(() => assertSupportedSchema({ type: "object", maxProperties: 0 }, "test schema"),
    /unsupported schema keyword maxProperties/);
  const contracts = Object.keys(registry.workloads).filter((role) => registry.workloads[role].kind === "contract");
  assert.deepEqual(contracts, ["dispatch", "worker-ingress", "connectors", "capture", "media", "speech", "llm", "delivery"]);
  assert.deepEqual(Object.values(registry.futureCapacity), [0, 0, 0, 0]);
  const versionSource = readFileSync(new URL("../internal/platform/postgres/migrate.go", import.meta.url), "utf8");
  assert.match(versionSource, new RegExp(`^const CurrentSchemaVersion int64 = ${registry.schemaVersion}$`, "m"));
  const cases = [
    [(input) => { input.version++; }, /version/],
    [(input) => { input.workloads.speech.kind = "deployment"; }, /speech.kind/],
    [(input) => { input.workloads.capture.port = 4545; }, /unsupported property port/],
    [(input) => { input.workloads.api.scale.admittedPods--; }, /exactly max replicas plus surge/],
    [(input) => { input.workloads.api.scale.pdbMin = 2; }, /PDB headroom/],
    [(input) => { input.workloads.api.lifecycle.graceSeconds = 15; }, /cover drain/],
    [(input) => { input.workloads.api.resources.requestCPU = 800; }, /resource boundary/],
    [(input) => { input.network.webPrefixPaths.push("/"); }, /keep health/],
    [(input) => { input.network.webExactPaths.push("/readyz"); }, /keep health/],
    [(input) => { input.futureCapacity.speechGPU = 1; }, /futureCapacity/],
  ];
  for (const [change, error] of cases) {
    const copy = structuredClone(registry);
    change(copy);
    assert.throws(() => loadRegistry(copy), error);
  }
});

test("external values cannot activate a role, inject pod fields, downgrade, or widen egress", () => {
  const cases = [
    [(input) => { input.images.dispatch = input.images.api; }, /unsupported property dispatch/],
    [(input) => { input.workloads = { speech: { enabled: true } }; }, /unsupported property workloads/],
    [(input) => { input.secrets.api = { databaseURL: "credential-canary" }; }, /secrets.api/],
    [(input) => { input.annotations = {}; }, /unsupported property annotations/],
    [(input) => { input.egress["worker-sql"] = ["192.0.2.3/32"]; }, /unsupported property worker-sql/],
    [(input) => { input.egress["api-pooler"] = ["0.0.0.0/0"]; }, /egress.api-pooler/],
    [(input) => { input.egress["api-pooler"] = ["bad/128"]; }, /specific IPv4 or IPv6/],
    [(input) => { input.images.api = "registry.example.com/tacit/api:latest"; }, /images.api/],
    [(input) => { input.schemaVersion = 3; }, /schemaVersion/],
    [(input) => { input.ingress.webhookHost = input.ingress.webHost; }, /dedicated hostname/],
    [(input) => { input.database.apiPool.mode = "session"; }, /apiPool.mode/],
  ];
  for (const [change, error] of cases) {
    const copy = structuredClone(bindings);
    change(copy);
    assert.throws(() => loadBindings(copy, registry), error);
  }
  assert.throws(() => loadBindings(null, registry), /must be an object/);
});

test("database projection counts hard admission, distinct backend pools, and overlapping direct slots", () => {
  const apiPool = bindings.database.apiPool;
  assert.deepEqual(projectDatabaseBudget(registry, bindings.database), {
    provider: "supabase",
    admittedAPIPods: 4,
    apiClients: 16,
    direct: { apiListen: 4, migration: 1, operational: 3, additional: 0 },
    pools: { '["pooler.example.com","postgres","tacit_api","transaction"]': { ...apiPool, clients: 16 } },
    clientsByPooler: { "pooler.example.com": 16 },
    providerReserved: 20,
    backendConnections: 43,
  });
  const samePool = { ...apiPool, clients: 6 };
  const sessionPool = { ...apiPool, mode: "session", clients: 4 };
  const rotatingPool = { ...apiPool, login: "tacit_api_next", clients: 16 };
  const project = (additionalPools, additionalDirect = 0) => projectDatabaseBudget(registry, {
    ...bindings.database, additionalPools, additionalDirect,
  });
  assert.equal(project([samePool]).backendConnections, 43);
  assert.equal(project([samePool]).clientsByPooler[apiPool.pooler], 22);
  assert.equal(project([sessionPool]).backendConnections, 58);
  assert.equal(project([rotatingPool]).backendConnections, 58);
  assert.throws(() => project([sessionPool, rotatingPool]), /backend budget 73/);
  assert.throws(() => project([rotatingPool], 2), /backend budget 60/);
  assert.throws(() => project([{ ...samePool, backendLimit: 14 }]), /conflicting backend limits/);
  assert.throws(() => project([{ ...samePool, clients: 184 }]), /client budget 200/);
});

test("commands require an explicit target and treat context and values as data", () => {
  assert.deepEqual(parseCLI(["deploy", "--file", "file name.json", "--environment", "example", "--context", "test/cluster"]), {
    action: "deploy", file: "file name.json", environment: "example", context: "test/cluster",
  });
  for (const context of ["--other", "x; touch test", "x$(id)"]) {
    assert.throws(() => parseCLI(["deploy", "--file", "file", "--environment", "example", `--context=${context}`]), /explicit/);
  }
  assert.throws(() => parseCLI(["rollback", "--file", "file"]), /explicit/);
  assert.throws(() => parseCLI(["validate", "--file", "file", "--phase", "migration"]), /only used by render/);
  assert.deepEqual(commandFor("kubectl", ["get", "namespaces"], { context: "test/cluster", namespace: "tacit-example-system" }),
    ["--context", "test/cluster", "-n", "tacit-example-system", "get", "namespaces"]);
  assert.deepEqual(commandFor("helm", ["rollback", "tacit-application", "2"], { context: "test/cluster", namespace: "tacit-example-system" }),
    ["rollback", "tacit-application", "2", "--kube-context", "test/cluster", "--namespace", "tacit-example-system"]);
});

test("compatible rollback requires the same schema and foundation bindings", () => {
  const prior = { ...bindings, revision: "aaaaaaaaaaaa" };
  assert.equal(requireCompatibleRevision(prior, bindings, registry), prior);
  for (const version of [undefined, 3, 5, "4"]) {
    assert.throws(() => requireCompatibleRevision({ ...prior, schemaVersion: version }, bindings, registry), /release schema is incompatible/);
  }
  assert.throws(() => requireCompatibleRevision({ ...prior, secrets: { ...prior.secrets, api: "older-secret" } }, bindings, registry),
    /incompatible with the current foundation/);
});

test("a missing Helm binary is reported without pretending a render occurred", () => {
  const result = spawnSync(process.execPath, ["scripts/platform.mjs", "render", "--file", "deploy/platform/example.json"], {
    cwd: new URL("../", import.meta.url), env: { ...process.env, PATH: "" }, encoding: "utf8",
  });
  assert.equal(result.status, 1);
  assert.equal(result.stdout, "");
  assert.match(result.stderr, /helm is required but was not found on PATH/);
});

test("an interrupted lock owner cannot continue or release another owner's lock", async () => {
  const calls = [];
  const kube = (label) => {
    calls.push(label);
    if (label === "read deployment lock") return '{"data":{"owner":"another-owner"}}';
    return "";
  };
  await assert.rejects(withEnvironmentLock(kube, async (check) => check(), "test-owner"), /ownership changed/);
  assert.deepEqual(calls, ["acquire deployment lock", "read deployment lock", "read deployment lock"]);
});

function releaseFixture({ installed = false, previousSchema = 4, fail } = {}) {
  const calls = [];
  let lockOwner;
  const active = { ...bindings, revision: "111111111111" };
  const previous = { ...bindings, revision: "222222222222", schemaVersion: previousSchema };
  const manifest = ({ phase, revision }) => `---\n# Source: fixture\n${stableJSON({
    kind: phase, metadata: { name: phase === "foundation" ? "foundation" : revision },
  })}\n`;
  function run(tool, args, options) {
    const { input, label } = options;
    calls.push({ tool, args, label, input });
    if (label === fail) throw new Error(label);
    if (label === "check Helm installation") return "v3.19.0";
    if (label.startsWith("render ")) return manifest(JSON.parse(input));
    if (label === "acquire deployment lock") lockOwner = args.at(-1).split("=")[2];
    if (label === "read deployment lock") return stableJSON({ data: { owner: lockOwner } });
    if (label === "list platform releases") return stableJSON(installed ? [
      { name: "tacit-foundation", revision: "1", status: "deployed" },
      { name: "tacit-application", revision: "2", status: "deployed" },
    ] : []);
    if (label === "read release bindings") {
      if (args.includes("tacit-foundation")) return stableJSON({ ...bindings, phase: "foundation" });
      return stableJSON(args[args.indexOf("--revision") + 1] === "1" ? previous : active);
    }
    if (label === "read installed foundation") return manifest({ ...bindings, phase: "foundation" });
    if (label === "read active application") return manifest(active);
    if (label === "read rollback target") return manifest(previous);
    if (label === "read application history") return stableJSON([
      { revision: 2, status: "deployed" }, { revision: 1, status: "superseded" },
    ]);
    if (label.endsWith(" quota")) {
      const pods = args.at(-3) === "migration" ? "1" : "4";
      const quota = { hard: { pods, "count/pods": pods } };
      return stableJSON({ spec: quota, status: quota });
    }
    if (label.endsWith(" secret binding")) return [...Object.values(registry.workloads).flatMap((workload) =>
      Object.values(workload.secretKeys ?? {})), "tls.crt", "tls.key"].join("\n");
    if (label.endsWith(" availability")) return '{"status":{"availableReplicas":2}}';
    return "";
  }
  return { run, calls };
}

const target = { environment: "example", context: "testing" };
const mutations = (calls) => calls.map((call) => call.label).filter((label) =>
  /^(?:install foundation|apply|remove completed|deploy application|rollback application|restore previous|remove unsuccessful)/u.test(label));

test("deploy installs foundation, waits for migration, deploys app, and returns a receipt", async () => {
  const fixture = releaseFixture();
  const smokes = [];
  const receipt = await changeRelease({ ...target, action: "deploy" }, registry, bindings, fixture.run,
    ({ revision }) => smokes.push(revision));
  assert.deepEqual(mutations(fixture.calls), ["install foundation", "apply migration job", "remove completed migration job", "deploy application"]);
  assert.ok(fixture.calls.findIndex(({ label }) => label === "ensure deployment namespace")
    < fixture.calls.findIndex(({ label }) => label === "acquire deployment lock"));
  assert.deepEqual(smokes, [bindings.revision]);
  assert.match(receipt.foundationHash, /^[0-9a-f]{64}$/u);
  assert.equal(receipt.schemaVersion, 4);
  const deploy = fixture.calls.find(({ label }) => label === "deploy application");
  assert.ok(deploy.args.includes("--atomic"));
  assert.ok(deploy.args.includes("testing"));
  assert.equal(JSON.parse(deploy.input).phase, "application");
  assert.equal(fixture.calls.at(-1).label, "release deployment lock");
  assert.equal(parseManifests(fixture.calls.find(({ label }) => label === "apply migration job").input)[0].kind, "migration");
});

test("foundation-only supports first-time secret provisioning without starting workloads", async () => {
  const fixture = releaseFixture();
  const receipt = await changeRelease({ ...target, action: "deploy", "foundation-only": true }, registry, bindings, fixture.run);
  assert.deepEqual(mutations(fixture.calls), ["install foundation"]);
  assert.equal(receipt.action, "foundation");
});

test("failed migration leaves foundation and never updates the application", async () => {
  const fixture = releaseFixture({ fail: "wait for migration job" });
  await assert.rejects(changeRelease({ ...target, action: "deploy" }, registry, bindings, fixture.run), /wait for migration job/);
  assert.deepEqual(mutations(fixture.calls), ["install foundation", "apply migration job"]);
});

test("a failed public smoke restores the prior compatible application", async () => {
  const fixture = releaseFixture({ installed: true });
  const smokes = [];
  await assert.rejects(changeRelease({ ...target, action: "deploy" }, registry, bindings, fixture.run, async ({ revision }) => {
    smokes.push(revision);
    if (revision === bindings.revision) throw new Error("web unhealthy");
  }), /previous application revision restored/);
  assert.deepEqual(mutations(fixture.calls), [
    "apply schema-check job", "remove completed schema-check job", "apply migration job", "remove completed migration job",
    "deploy application", "restore previous application after failed smoke",
  ]);
  assert.deepEqual(smokes, [bindings.revision, "111111111111"]);
});

test("rollback verifies schema using the deployed image and never changes foundation", async () => {
  const fixture = releaseFixture({ installed: true });
  const receipt = await changeRelease({ ...target, action: "rollback" }, registry, bindings, fixture.run, async () => {});
  assert.deepEqual(mutations(fixture.calls), ["apply schema-check job", "remove completed schema-check job", "rollback application"]);
  assert.equal(JSON.parse(fixture.calls.find(({ label }) => label === "render schema-check").input).revision, "111111111111");
  assert.equal(receipt.targetHelmRevision, "1");
  assert.equal(receipt.revision, "222222222222");
});

test("rollback refuses incompatible history and a failed read-only schema check before app changes", async () => {
  const incompatible = releaseFixture({ installed: true, previousSchema: 3 });
  await assert.rejects(changeRelease({ ...target, action: "rollback" }, registry, bindings, incompatible.run), /release schema is incompatible/);
  assert.deepEqual(mutations(incompatible.calls), []);
  const checkFailed = releaseFixture({ installed: true, fail: "wait for schema-check job" });
  await assert.rejects(changeRelease({ ...target, action: "rollback" }, registry, bindings, checkFailed.run), /wait for schema-check job/);
  assert.deepEqual(mutations(checkFailed.calls), ["apply schema-check job"]);
});
