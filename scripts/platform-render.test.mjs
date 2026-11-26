import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { chart, example, loadBindings, loadRegistry, phases, readJSON } from "./platform-config.mjs";
import { parseManifests, renderPhase } from "./platform.mjs";

const registry = loadRegistry();
const bindings = loadBindings(readJSON(example), registry);
const helm = spawnSync("helm", ["version", "--short"], { encoding: "utf8" });
const skip = helm.error?.code === "ENOENT" && process.env.TACIT_REQUIRE_HELM !== "1" ? "Helm is not installed" : false;
const namespace = (role) => `tacit-${bindings.environment}-${role}`;
const workloadLabels = (role) => ({ "app.kubernetes.io/name": "tacit", "tacit.dev/role": role });

function resource(objects, kind, name, role) {
  const found = objects.filter((object) => object.kind === kind && object.metadata.name === name
    && (role === undefined || object.metadata.namespace === namespace(role)));
  assert.equal(found.length, 1, `expected exactly one ${kind}/${name} in ${role ?? "the cluster"}`);
  return found[0];
}

function assertPod(template, role) {
  const workload = registry.workloads[role];
  const { spec } = template;
  assert.deepEqual(template.metadata.labels, workloadLabels(role));
  assert.equal(template.metadata.annotations["tacit.dev/schema-version"], String(registry.schemaVersion));
  assert.equal(spec.serviceAccountName, role);
  assert.equal(spec.automountServiceAccountToken, false);
  assert.equal(spec.terminationGracePeriodSeconds, workload.lifecycle.graceSeconds);
  assert.deepEqual(spec.securityContext, {
    runAsNonRoot: true, runAsUser: workload.uid, runAsGroup: workload.gid, fsGroup: workload.gid,
    seccompProfile: { type: registry.security.seccompProfile },
  });
  assert.equal(spec.initContainers, undefined);
  assert.equal(spec.containers.length, 1);
  const [container] = spec.containers;
  assert.deepEqual(container.securityContext, {
    allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: ["ALL"] },
  });
  assert.equal(container.image, bindings.images[workload.artifact]);
  assert.equal(container.envFrom, undefined);
  const refs = container.env.filter((entry) => entry.valueFrom);
  assert.equal(refs.length, Object.keys(workload.secretKeys).length);
  for (const [name, key] of Object.entries(workload.secretKeys)) {
    assert.deepEqual(refs.find((entry) => entry.name === name), {
      name, valueFrom: { secretKeyRef: { name: bindings.secrets[role], key } },
    });
  }
  assert.deepEqual(Object.fromEntries((container.volumeMounts ?? []).map((mount) => [mount.name, mount.mountPath])), workload.writable);
  const volumes = spec.volumes ?? [];
  assert.equal(volumes.length, Object.keys(workload.writable).length);
  const scratch = volumes.reduce((total, volume) => total + Number.parseInt(volume.emptyDir.sizeLimit, 10), 0);
  assert.ok(scratch <= workload.resources.scratch);
  return container;
}

test("Helm lints, renders deterministically, and excludes unsupported values", { skip }, () => {
  assert.equal(helm.status, 0, helm.error?.message);
  for (const phase of [...phases, "schema-check"]) {
    const lint = spawnSync("helm", ["lint", fileURLToPath(chart), "--kube-version", "1.32.0", "-f", fileURLToPath(example),
      "--set", `phase=${phase}`], { encoding: "utf8" });
    assert.equal(lint.status, 0, lint.stderr || lint.stdout);
    assert.equal(renderPhase(bindings, phase), renderPhase(bindings, phase));
  }
  for (const expression of ["workloads.speech.enabled=true", "images.api=registry.example.com/tacit/api:latest", "podAnnotations.example=test"]) {
    const render = spawnSync("helm", ["template", "rejected", fileURLToPath(chart), "-f", fileURLToPath(example),
      "--set", expression], { encoding: "utf8" });
    assert.notEqual(render.status, 0, expression);
    assert.match(render.stderr, /values don't meet the specifications/u);
  }
});

test("rendered foundation binds roles to exact, bounded capability and network policy", { skip }, () => {
  const foundation = parseManifests(renderPhase(bindings, "foundation"));
  const enabled = Object.entries(registry.workloads).filter(([, workload]) => workload.kind !== "contract");
  assert.ok(foundation.every((object) => ["Namespace", "ServiceAccount", "ResourceQuota", "NetworkPolicy"].includes(object.kind)));
  assert.deepEqual(foundation.filter((object) => object.kind === "Namespace").map((object) => object.metadata.name).sort(),
    enabled.map(([role]) => namespace(role)).sort());
  for (const [role, workload] of enabled) {
    const ns = resource(foundation, "Namespace", namespace(role));
    assert.equal(ns.metadata.labels["pod-security.kubernetes.io/enforce"], "restricted");
    assert.equal(ns.metadata.labels["pod-security.kubernetes.io/enforce-version"], registry.security.podSecurityVersion);
    assert.equal(resource(foundation, "ServiceAccount", role, role).automountServiceAccountToken, false);
    const { spec } = resource(foundation, "ResourceQuota", role, role);
    const pods = workload.scale?.admittedPods ?? workload.admittedPods;
    assert.deepEqual(spec, { hard: {
      pods: String(pods), "count/pods": String(pods),
      "requests.cpu": `${pods * workload.resources.requestCPU}m`, "limits.cpu": `${pods * workload.resources.limitCPU}m`,
      "requests.memory": `${pods * workload.resources.requestMemory}Mi`, "limits.memory": `${pods * workload.resources.limitMemory}Mi`,
      "requests.ephemeral-storage": `${pods * workload.resources.scratch}Mi`, "limits.ephemeral-storage": `${pods * workload.resources.scratch}Mi`,
    } });
    const denied = resource(foundation, "NetworkPolicy", "default-deny", role);
    assert.deepEqual(denied.spec, { podSelector: {}, policyTypes: ["Ingress", "Egress"] });
  }
  const peer = (target, labels) => ({
    namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": target } },
    podSelector: { matchLabels: labels },
  });
  const edges = registry.network.edges;
  for (const role of edges.ingress) {
    const rule = resource(foundation, "NetworkPolicy", "from-ingress", role);
    assert.deepEqual(rule.spec.ingress, [{
      from: [peer(registry.network.ingressPeer.namespace, registry.network.ingressPeer.labels)],
      ports: [{ protocol: "TCP", port: registry.workloads[role].port }],
    }]);
  }
  for (const role of edges.dns) {
    const rule = resource(foundation, "NetworkPolicy", "to-dns", role);
    assert.deepEqual(rule.spec.egress, [{
      to: [peer(registry.network.dnsPeer.namespace, registry.network.dnsPeer.labels)],
      ports: [{ protocol: "UDP", port: 53 }, { protocol: "TCP", port: 53 }],
    }]);
  }
  for (const { from, to } of edges.internal) {
    const ports = [{ protocol: "TCP", port: registry.workloads[to].port }];
    const sender = resource(foundation, "NetworkPolicy", `to-${to}`, from);
    const receiver = resource(foundation, "NetworkPolicy", `from-${from}`, to);
    assert.deepEqual(sender.spec.egress, [{ to: [peer(namespace(to), workloadLabels(to))], ports }]);
    assert.deepEqual(receiver.spec.ingress, [{ from: [peer(namespace(from), workloadLabels(from))], ports }]);
  }
  for (const [name, { from, port }] of Object.entries(edges.external)) {
    assert.deepEqual(resource(foundation, "NetworkPolicy", name, from).spec, {
      podSelector: { matchLabels: workloadLabels(from) },
      policyTypes: ["Egress"],
      egress: [{ to: bindings.egress[name].map((cidr) => ({ ipBlock: { cidr } })), ports: [{ protocol: "TCP", port }] }],
    });
  }
  assert.equal(foundation.filter((object) => object.kind === "NetworkPolicy").length,
    enabled.length + edges.ingress.length + edges.dns.length + edges.internal.length * 2 + Object.keys(edges.external).length);
});

test("rendered deployments and migration are compatible with foundation and stay inside quota", { skip }, () => {
  const application = parseManifests(renderPhase(bindings, "application"));
  assert.equal(application.length, 10);
  for (const role of ["api", "web"]) {
    const workload = registry.workloads[role];
    const { spec } = resource(application, "Deployment", role, role);
    const container = assertPod(spec.template, role);
    assert.equal(spec.strategy.rollingUpdate.maxSurge, workload.scale.surge);
    assert.equal(spec.strategy.rollingUpdate.maxUnavailable, 0);
    assert.equal(spec.replicas, undefined);
    assert.equal(container.livenessProbe.httpGet.path, workload.lifecycle.live);
    assert.equal(container.readinessProbe.httpGet.path, workload.lifecycle.ready);
    assert.deepEqual(container.ports, [{ containerPort: workload.port, name: "http" }]);
    const service = resource(application, "Service", role, role).spec;
    assert.deepEqual(service.selector, spec.selector.matchLabels);
    assert.equal(service.ports[0].port, workload.port);
    assert.equal(service.sessionAffinity, "None");
    const hpa = resource(application, "HorizontalPodAutoscaler", role, role).spec;
    assert.equal(hpa.minReplicas, workload.scale.min);
    assert.equal(hpa.maxReplicas, workload.scale.max);
    assert.equal(hpa.metrics[0].resource.target.averageUtilization, workload.scale.cpuTarget);
    assert.equal(resource(application, "PodDisruptionBudget", role, role).spec.minAvailable, workload.scale.pdbMin);
    const { rules, ingressClassName } = resource(application, "Ingress", role, role).spec;
    assert.equal(ingressClassName, registry.network.ingressClass);
    assert.equal(rules.length, 1);
    assert.equal(rules[0].host, role === "api" ? bindings.ingress.webhookHost : bindings.ingress.webHost);
    assert.ok(rules[0].http.paths.every((path) => path.backend.service.name === role));
    const paths = rules[0].http.paths.map(({ path, pathType }) => [path, pathType]);
    if (role === "api") {
      assert.deepEqual(paths, [[registry.network.webhookPath, "Exact"]]);
      assert.equal(container.env.find(({ name }) => name === "TACIT_DATABASE_MAX_CONNECTIONS").value, String(workload.database.poolPerPod));
      assert.ok(container.args.includes(`--drain-timeout=${workload.lifecycle.drainSeconds}s`));
    } else {
      assert.deepEqual(paths, [
        ...registry.network.webExactPaths.map((path) => [path, "Exact"]),
        ...registry.network.webPrefixPaths.map((path) => [path, "Prefix"]),
      ]);
      assert.equal(container.env.find(({ name }) => name === "TACIT_API_ORIGIN").value, `http://api.${namespace("api")}.svc.cluster.local:8080`);
      assert.equal(container.args, undefined);
      assert.equal(container.command, undefined);
    }
  }
  for (const [phase, action] of [["migration", "migrate"], ["schema-check", "verify-schema"]]) {
    const jobs = parseManifests(renderPhase(bindings, phase));
    assert.equal(jobs.length, 1);
    const job = resource(jobs, "Job", `tacit-${action}-${bindings.revision}`, "migration");
    assert.equal(job.metadata.annotations, undefined);
    assert.equal(job.spec.backoffLimit, 0);
    assert.equal(job.spec.parallelism, registry.workloads.migration.admittedPods);
    assert.equal(job.spec.activeDeadlineSeconds, registry.workloads.migration.lifecycle.deadlineSeconds);
    assert.equal(job.spec.template.spec.restartPolicy, "Never");
    assert.deepEqual(assertPod(job.spec.template, "migration").args, [action]);
  }
});
