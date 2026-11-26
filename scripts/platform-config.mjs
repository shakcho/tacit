import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { isIP } from "node:net";
import { isDeepStrictEqual } from "node:util";

export const chart = new URL("../deploy/charts/tacit/", import.meta.url);
export const example = new URL("../deploy/platform/example.json", import.meta.url);
export const phases = ["foundation", "migration", "application"];

export function readJSON(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (error) {
    throw new Error(`cannot read JSON from ${path} (${error.code ?? "invalid JSON"})`);
  }
}

const registrySchema = readJSON(new URL("files/platform.v1.schema.json", chart));
const valuesSchema = readJSON(new URL("values.schema.json", chart));

// The packaged schemas share this subset of Draft 7 with Helm.
function checkShape(value, schema, root, path) {
  function require(condition, expectation) {
    if (!condition) throw new Error(`${path} ${expectation}`);
  }

  if (schema.$ref) {
    checkShape(value, root.definitions[schema.$ref.split("/").at(-1)], root, path);
    return;
  }
  if (Object.hasOwn(schema, "const")) require(isDeepStrictEqual(value, schema.const), "has an unsupported value");
  if (schema.enum) require(schema.enum.includes(value), "has an unsupported value");
  switch (schema.type) {
    case "object": {
      require(value !== null && typeof value === "object" && !Array.isArray(value), "must be an object");
      for (const name of schema.required ?? []) require(Object.hasOwn(value, name), `requires ${name}`);
      require(Object.keys(value).length >= (schema.minProperties ?? 0), "has too few properties");
      for (const [name, property] of Object.entries(value)) {
        const known = Object.hasOwn(schema.properties ?? {}, name);
        require(known || schema.additionalProperties !== false, `has unsupported property ${name}`);
        const rule = known ? schema.properties[name] : schema.additionalProperties;
        if (rule) checkShape(property, rule, root, `${path}.${name}`);
      }
      break;
    }
    case "array":
      require(Array.isArray(value), "must be an array");
      require(value.length >= (schema.minItems ?? 0) && value.length <= (schema.maxItems ?? Infinity), "has invalid length");
      if (schema.uniqueItems) require(new Set(value).size === value.length, "must contain unique items");
      for (const [index, item] of value.entries()) checkShape(item, schema.items, root, `${path}[${index}]`);
      break;
    case "integer":
      require(Number.isSafeInteger(value) && value >= schema.minimum && value <= schema.maximum, "is outside the integer bounds");
      break;
    case "string":
      require(typeof value === "string" && value.length >= (schema.minLength ?? 0) && value.length <= (schema.maxLength ?? Infinity), "has invalid string length");
      if (schema.pattern) require(new RegExp(schema.pattern, "u").test(value), "has an invalid format");
      break;
  }
}

export function loadRegistry(input = readJSON(new URL("files/platform.v1.json", chart))) {
  checkShape(input, registrySchema, registrySchema, "registry");
  for (const [role, workload] of Object.entries(input.workloads)) {
    if (workload.kind === "contract") continue;
    const { resources, scale, lifecycle } = workload;
    if (resources.requestCPU > resources.limitCPU || resources.requestMemory > resources.limitMemory
      || resources.scratch < Object.keys(workload.writable).length) {
      throw new Error(`${role} resource requests or writable volumes exceed the resource boundary`);
    }
    if (workload.kind === "deployment" && (scale.admittedPods !== scale.max + scale.surge
      || scale.pdbMin >= scale.min || scale.min > scale.max)) {
      throw new Error(`${role} quota must cover exactly max replicas plus surge, with PDB headroom`);
    }
    if (lifecycle.drainSeconds && lifecycle.graceSeconds < lifecycle.drainSeconds + 15) {
      throw new Error(`${role} grace must cover drain plus 15 seconds`);
    }
  }
  const { webExactPaths, webPrefixPaths } = input.network;
  if (!webExactPaths.includes("/") || webPrefixPaths.includes("/")
    || [...webExactPaths, ...webPrefixPaths].some((path) => /^\/(?:healthz|readyz|webhooks)(?:\/|$)/u.test(path))) {
    throw new Error("web ingress must keep health, readiness, and API webhook paths internal");
  }
  return input;
}

export function loadBindings(input, registry) {
  checkShape(input, valuesSchema, valuesSchema, "bindings");
  if (input.schemaVersion !== registry.schemaVersion) throw new Error("schema-version mismatch");
  if (input.ingress.webHost === input.ingress.webhookHost) throw new Error("webhook ingress needs a dedicated hostname");
  for (const [edge, cidrs] of Object.entries(input.egress)) {
    for (const entry of cidrs) {
      const [address, prefix] = entry.split("/");
      const family = Number(prefix) === 32 ? 4 : 6;
      if (isIP(address) !== family || ["0.0.0.0", "::"].includes(address)) {
        throw new Error(`egress ${edge} needs a specific IPv4 or IPv6 host address`);
      }
    }
  }
  projectDatabaseBudget(registry, input.database);
  return input;
}

export function projectDatabaseBudget(registry, bindings) {
  const { api, migration } = registry.workloads;
  const limits = registry.database;
  // ResourceQuota counts nonterminal pods, including pods held in deletion.
  const admittedAPIPods = api.scale.admittedPods;
  const apiClients = admittedAPIPods * api.database.poolPerPod;
  const direct = {
    apiListen: admittedAPIPods * api.database.directPerPod,
    migration: migration.admittedPods * migration.database.directPerPod,
    operational: limits.otherDirectHeadroom,
    additional: bindings.additionalDirect,
  };

  const pools = new Map();
  const clientsByPooler = new Map();
  for (const pool of [{ ...bindings.apiPool, clients: apiClients }, ...bindings.additionalPools]) {
    const identity = [pool.pooler, pool.database, pool.login, pool.mode];
    const key = JSON.stringify(identity);
    const known = pools.get(key);
    if (known !== undefined && known.backendLimit !== pool.backendLimit) {
      throw new Error(`conflicting backend limits for pool tuple ${key}`);
    }
    pools.set(key, { ...pool, clients: (known?.clients ?? 0) + pool.clients });
    clientsByPooler.set(pool.pooler, (clientsByPooler.get(pool.pooler) ?? 0) + pool.clients);
  }
  const backendConnections = limits.providerReservedConnections
    + [...pools.values()].reduce((sum, pool) => sum + pool.backendLimit, 0)
    + Object.values(direct).reduce((sum, count) => sum + count, 0);
  if (backendConnections >= limits.maxBackendConnections) {
    throw new Error(`backend budget ${backendConnections} must stay below database capacity ${limits.maxBackendConnections}`);
  }
  for (const [pooler, count] of clientsByPooler) {
    if (count >= limits.maxClientsPerPooler) throw new Error(`client budget ${count} reaches pooler capacity at ${pooler}`);
  }
  return {
    provider: limits.provider, admittedAPIPods, apiClients, direct,
    pools: Object.fromEntries([...pools].sort()), clientsByPooler: Object.fromEntries([...clientsByPooler].sort()),
    providerReserved: limits.providerReservedConnections, backendConnections,
  };
}

export function stableJSON(input) {
  if (Array.isArray(input)) return `[${input.map(stableJSON).join(",")}]`;
  if (input !== null && typeof input === "object") {
    return `{${Object.keys(input).sort().map((key) => `${JSON.stringify(key)}:${stableJSON(input[key])}`).join(",")}}`;
  }
  return JSON.stringify(input);
}

export function fingerprint(value) {
  return createHash("sha256").update(stableJSON(value)).digest("hex");
}

export function foundationBindings(registry, { environment, ingress, egress, secrets, database }) {
  return fingerprint({ registry, environment, ingress, egress, secrets, database });
}
