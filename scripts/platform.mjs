import { spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

import {
  chart, fingerprint, foundationBindings, loadBindings, loadRegistry, phases,
  projectDatabaseBudget, readJSON, stableJSON,
} from "./platform-config.mjs";

const chartPath = fileURLToPath(chart);
const foundationRelease = "tacit-foundation";
const applicationRelease = "tacit-application";
const lockName = "tacit-platform-lock";
const timeout = "--timeout=5m";

export function parseCLI(args) {
  const [action, ...flags] = args;
  if (!["validate", "render", "deploy", "rollback"].includes(action)) {
    throw new Error("usage: platform.mjs validate|render|deploy|rollback --file PATH [--environment NAME --context NAME]");
  }
  const { values } = parseArgs({
    args: flags,
    options: {
      file: { type: "string" }, environment: { type: "string" }, context: { type: "string" },
      phase: { type: "string" }, "foundation-only": { type: "boolean" },
    },
  });
  if (!values.file) throw new Error("--file is required");
  const mutating = action === "deploy" || action === "rollback";
  if (mutating && (!values.environment || !values.context || !/^[a-zA-Z0-9_.:/@-]+$/u.test(values.context) || values.context.startsWith("-"))) {
    throw new Error("deploy and rollback require an explicit --context and --environment");
  }
  if (!mutating && (values.environment || values.context)) throw new Error("target selection is only used by deploy and rollback");
  if (values.phase && (action !== "render" || ![...phases, "schema-check"].includes(values.phase))) {
    throw new Error("--phase is only used by render and must name a chart phase");
  }
  if (values["foundation-only"] && action !== "deploy") throw new Error("--foundation-only is only used by deploy");
  return { action, ...values };
}

function execute(tool, args, { input, label }) {
  const result = spawnSync(tool, args, {
    input, encoding: "utf8", shell: false, maxBuffer: 8 * 1024 * 1024, timeout: 720_000,
  });
  if (result.error?.code === "ENOENT") throw new Error(`${tool} is required but was not found on PATH`);
  if (result.error) throw new Error(`${label}: ${tool} could not finish (${result.error.code ?? "execution error"})`);
  if (result.status !== 0) {
    // Tool diagnostics may contain cluster data.
    throw new Error(`${label}: ${tool} exited with ${result.status ?? result.signal}`);
  }
  return result.stdout;
}

export function commandFor(tool, args, { context, namespace }) {
  return tool === "kubectl" ? ["--context", context, "-n", namespace, ...args]
    : [...args, "--kube-context", context, "--namespace", namespace];
}

function phaseValues(bindings, phase) {
  return stableJSON({ ...bindings, phase });
}

function roleNamespace({ environment }, role) {
  return `tacit-${environment}-${role}`;
}

export function renderPhase(bindings, phase, run = execute) {
  const release = phase === "foundation" ? foundationRelease : applicationRelease;
  return run("helm", ["template", release, chartPath, "--namespace", roleNamespace(bindings, "system"), "--kube-version", "1.32.0", "-f", "-"], {
    input: phaseValues(bindings, phase), label: `render ${phase}`,
  });
}

export function parseManifests(text) {
  return text.split(/^---\s*$/mu).map((chunk) => chunk.replace(/^#[^\n]*$/gmu, "").trim()).filter(Boolean).map((chunk) => {
    const value = JSON.parse(chunk);
    if (value === null || typeof value !== "object" || typeof value.kind !== "string"
      || typeof value.metadata?.name !== "string") throw new Error("invalid rendered resource");
    return value;
  });
}

function manifestDigest(text) {
  return fingerprint(parseManifests(text).map(stableJSON).sort());
}

export function requireCompatibleRevision(raw, current, registry) {
  if (raw === null || typeof raw !== "object" || raw.schemaVersion !== registry.schemaVersion) {
    throw new Error(`release schema is incompatible with schema version ${registry.schemaVersion}`);
  }
  const prior = loadBindings(raw, registry);
  if (prior.phase !== "application" || foundationBindings(registry, prior) !== foundationBindings(registry, current)) {
    throw new Error("release bindings are incompatible with the current foundation");
  }
  return prior;
}

export async function withEnvironmentLock(kube, work, owner = randomUUID()) {
  kube("acquire deployment lock", ["create", "configmap", lockName, `--from-literal=owner=${owner}`]);
  function assertOwner() {
    const lock = JSON.parse(kube("read deployment lock", ["get", "configmap", lockName, "-o", "json"]));
    if (lock?.data?.owner !== owner) throw new Error("deployment lock ownership changed");
  }
  let workError;
  try {
    return await work(assertOwner);
  } catch (error) {
    workError = error;
    throw error;
  } finally {
    try {
      assertOwner();
      kube("release deployment lock", ["delete", "configmap", lockName]);
    } catch (error) {
      if (workError) throw new Error(`${workError.message}; inspect the deployment lock before retrying (${error.message})`);
      throw error;
    }
  }
}

function releaseStates(helm) {
  const states = JSON.parse(helm("list platform releases", ["list", "--all", "--filter", "^tacit-(foundation|application)$", "-o", "json"]));
  if (!Array.isArray(states) || states.some((row) => ![foundationRelease, applicationRelease].includes(row?.name)
    || !/^[1-9][0-9]*$/u.test(String(row.revision)) || typeof row.status !== "string")) {
    throw new Error("invalid Helm release inventory");
  }
  return new Map(states.map((row) => [row.name, { revision: String(row.revision), status: row.status }]));
}

function releaseValues(helm, release, revision) {
  return JSON.parse(helm("read release bindings", ["get", "values", release, "--all", "--revision", revision, "-o", "json"]));
}

function previousRevision(helm, current) {
  const history = JSON.parse(helm("read application history", ["history", applicationRelease, "-o", "json"]));
  if (!Array.isArray(history) || history.some((row) => !Number.isSafeInteger(row?.revision) || row.revision < 1)) {
    throw new Error("invalid Helm application history");
  }
  const revision = Math.max(0, ...history.filter((row) => row.status === "superseded" && row.revision < Number(current)).map((row) => row.revision));
  if (!revision) throw new Error("no prior successful application revision is available");
  return String(revision);
}

function verifySecretKeys(kube, role, name, keys) {
  const fields = kube(`check ${role} secret binding`, ["get", "secret", name, "-o",
    'go-template={{range $key,$value := .data}}{{$key}}{{"\n"}}{{end}}'], role).trim().split("\n");
  if (keys.some((key) => !fields.includes(key))) throw new Error(`${role} secret ${name} lacks required keys`);
}

async function waitForQuotas(kube, registry) {
  for (const [role, workload] of Object.entries(registry.workloads)) {
    if (workload.kind === "contract") continue;
    const expected = String(workload.kind === "job" ? workload.admittedPods : workload.scale.admittedPods);
    for (let attempt = 0; ; attempt++) {
      const quota = JSON.parse(kube(`check ${role} quota`, ["get", "resourcequota", role, "-o", "json"], role));
      if (["pods", "count/pods"].every((key) => quota?.spec?.hard?.[key] === expected
        && quota?.status?.hard?.[key] === expected)) break;
      if (attempt === 15) throw new Error(`${role} pod quota is not enforced at ${expected}`);
      await delay(2_000);
    }
  }
}

function schemaJob(kube, assertOwner, registry, bindings, phase, manifest) {
  const action = phase === "migration" ? "migrate" : "verify-schema";
  const name = `tacit-${action}-${bindings.revision}`;
  const deadline = registry.workloads.migration.lifecycle.deadlineSeconds;
  assertOwner();
  kube(`apply ${phase} job`, ["apply", "--server-side", "-f", "-"], "migration", manifest);
  // A failed Job remains available for inspection until TTL cleanup.
  kube(`wait for ${phase} job`, ["wait", `job/${name}`, "--for=condition=Complete", `--timeout=${deadline + 30}s`], "migration");
  assertOwner();
  kube(`remove completed ${phase} job`, ["delete", `job/${name}`, "--cascade=foreground", "--wait=true", "--timeout=90s"], "migration");
}

async function publicSmokes({ ingress }) {
  const check = (host, path, init) => fetch(`https://${host}${path}`, {
    ...init, redirect: "manual", signal: AbortSignal.timeout(3_000),
  });
  for (let attempt = 0; attempt < 10; attempt++) {
    try {
      const web = await check(ingress.webHost, "/");
      const location = web.headers.get("location");
      const acceptable = web.ok || (location && [307, 308].includes(web.status) && new URL(location, web.url).pathname === "/meetings");
      void web.body?.cancel().catch(() => undefined);
      if (!acceptable) throw new Error("web route");
      const webhook = await check(ingress.webhookHost, "/webhooks/workos", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: "{}",
      });
      if (webhook.status !== 400 || (await webhook.json())?.code !== "INVALID_WEBHOOK") throw new Error("webhook route");
      for (const [host, path] of [[ingress.webHost, "/healthz"], [ingress.webHost, "/readyz"],
        [ingress.webhookHost, "/v1/auth/session"], [ingress.webhookHost, "/webhooks/workos/extra"]]) {
        const response = await check(host, path);
        void response.body?.cancel().catch(() => undefined);
        if (response.status !== 404) throw new Error("restricted public route");
      }
      return;
    } catch {
      if (attempt === 9) throw new Error("public ingress smokes failed");
      await delay(2_000);
    }
  }
}

async function rollOut(kube, registry) {
  for (const [role, workload] of Object.entries(registry.workloads)) {
    if (workload.kind === "deployment") {
      kube(`wait for ${role} rollout`, ["rollout", "status", `deployment/${role}`, timeout], role);
      for (let attempt = 0; ; attempt++) {
        const deployment = JSON.parse(kube(`check ${role} availability`, ["get", "deployment", role, "-o", "json"], role));
        if (Number.isSafeInteger(deployment?.status?.availableReplicas)
          && deployment.status.availableReplicas >= workload.scale.min) break;
        if (attempt === 15) throw new Error(`${role} did not reach its availability floor`);
        await delay(2_000);
      }
    }
  }
}

export async function changeRelease(options, registry, bindings, run = execute, smoke = publicSmokes) {
  if (options.environment !== bindings.environment) throw new Error("--environment must match the bindings file");
  const system = roleNamespace(bindings, "system");
  const helm = (label, args, input) => run("helm", commandFor("helm", args, { namespace: system, context: options.context }), { input, label });
  const kube = (label, args, role = "system", input) => run("kubectl", commandFor("kubectl", args, {
    context: options.context, namespace: roleNamespace(bindings, role),
  }), { input, label });
  const version = run("helm", ["version", "--short"], { label: "check Helm installation" });
  const major = /^v([34])\./u.exec(version)?.[1];
  if (!major) throw new Error("platform requires Helm 3 or 4");
  run("kubectl", ["version", "--client"], { label: "check kubectl installation" });
  const foundation = renderPhase(bindings, "foundation", run);
  const app = renderPhase(bindings, "application", run);
  const migration = options.action === "deploy" && !options["foundation-only"] ? renderPhase(bindings, "migration", run) : undefined;
  const foundationHash = manifestDigest(foundation);
  kube("check deployment namespace", ["get", "namespace", system, "-o", "name"]);

  return withEnvironmentLock(kube, async (assertOwner) => {
    const states = releaseStates(helm);
    const foundationState = states.get(foundationRelease);
    const appState = states.get(applicationRelease);
    for (const [release, state] of states) {
      if (state.status !== "deployed") throw new Error(`${release} release is ${state.status}; inspect it before continuing`);
    }
    if (options.action === "rollback" && !appState) throw new Error("no deployed application to roll back");
    if (foundationState) {
      const installed = loadBindings(releaseValues(helm, foundationRelease, foundationState.revision), registry);
      if (installed.phase !== "foundation" || foundationBindings(registry, installed) !== foundationBindings(registry, bindings)
        || manifestDigest(helm("read installed foundation", ["get", "manifest", foundationRelease])) !== foundationHash) {
        throw new Error("requested bindings differ from the installed foundation");
      }
    } else {
      if (appState) throw new Error("deployed application requires the installed foundation");
      assertOwner();
      helm("install foundation", ["upgrade", "--install", foundationRelease, chartPath, "-f", "-", "--wait", timeout], phaseValues(bindings, "foundation"));
    }
    await waitForQuotas(kube, registry);
    const receipt = { environment: bindings.environment, context: options.context, registryVersion: registry.version,
      schemaVersion: registry.schemaVersion, foundationHash };
    if (options["foundation-only"]) return { ...receipt, action: "foundation" };

    for (const [role, workload] of Object.entries(registry.workloads)) {
      if (workload.kind !== "contract") verifySecretKeys(kube, role, bindings.secrets[role], Object.values(workload.secretKeys));
    }
    verifySecretKeys(kube, "web", bindings.ingress.webTLSSecret, ["tls.crt", "tls.key"]);
    verifySecretKeys(kube, "api", bindings.ingress.webhookTLSSecret, ["tls.crt", "tls.key"]);
    let prior;
    if (appState) {
      prior = requireCompatibleRevision(releaseValues(helm, applicationRelease, appState.revision), bindings, registry);
      if (manifestDigest(helm("read active application", ["get", "manifest", applicationRelease])) !== manifestDigest(renderPhase(prior, "application", run))) {
        throw new Error("active application template is incompatible with the current platform");
      }
    }

    if (options.action === "rollback") {
      const target = previousRevision(helm, appState.revision);
      const selected = requireCompatibleRevision(releaseValues(helm, applicationRelease, target), bindings, registry);
      const manifest = helm("read rollback target", ["get", "manifest", applicationRelease, "--revision", target]);
      if (manifestDigest(manifest) !== manifestDigest(renderPhase(selected, "application", run))) {
        throw new Error("rollback target template is incompatible with the current platform");
      }
      schemaJob(kube, assertOwner, registry, prior, "schema-check", renderPhase(prior, "schema-check", run));
      assertOwner();
      helm("rollback application", ["rollback", applicationRelease, target, "--wait", timeout]);
      await rollOut(kube, registry);
      await smoke(selected);
      return { ...receipt, action: "rollback", revision: selected.revision, targetHelmRevision: target, applicationHash: manifestDigest(manifest) };
    }

    if (prior) schemaJob(kube, assertOwner, registry, prior, "schema-check", renderPhase(prior, "schema-check", run));
    schemaJob(kube, assertOwner, registry, bindings, "migration", migration);
    assertOwner();
    const recoverOnFailure = major === "4" ? "--rollback-on-failure" : "--atomic";
    helm("deploy application", ["upgrade", "--install", applicationRelease, chartPath, "-f", "-", recoverOnFailure, timeout], phaseValues(bindings, "application"));
    try {
      await rollOut(kube, registry);
      await smoke(bindings);
    } catch (error) {
      assertOwner();
      if (prior) {
        helm("restore previous application after failed smoke", ["rollback", applicationRelease, appState.revision, "--wait", timeout]);
        await rollOut(kube, registry);
        await smoke(prior);
        throw new Error(`${error.message}; previous application revision restored`);
      }
      helm("remove unsuccessful first application release", ["uninstall", applicationRelease, "--wait", timeout]);
      throw new Error(`${error.message}; first application release removed`);
    }
    return { ...receipt, action: "deploy", revision: bindings.revision, applicationHash: manifestDigest(app) };
  });
}

async function main() {
  const options = parseCLI(process.argv.slice(2));
  const registry = loadRegistry();
  const bindings = loadBindings(readJSON(resolve(options.file)), registry);
  if (options.action === "render") {
    process.stdout.write((options.phase ? [options.phase] : phases).map((phase) => renderPhase(bindings, phase)).join("\n"));
    return;
  }
  const result = options.action === "validate" ? {
    environment: bindings.environment, registryVersion: registry.version, schemaVersion: registry.schemaVersion,
    foundationBindings: foundationBindings(registry, bindings), budget: projectDatabaseBudget(registry, bindings.database),
    capabilities: Object.fromEntries(Object.entries(registry.workloads).map(([role, work]) =>
      [role, work.kind === "contract" ? "disabled" : "runnable"])),
  } : await changeRelease(options, registry, bindings);
  process.stdout.write(`${stableJSON(result)}\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(`platform: ${error.message}`);
    process.exitCode = 1;
  });
}
