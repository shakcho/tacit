import { spawn } from "node:child_process";
import process from "node:process";

try {
  process.loadEnvFile(".env");
} catch (error) {
  if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
}

const webHost = process.env.TACIT_WEB_HOST ?? "127.0.0.1";
const webPort = process.env.TACIT_WEB_PORT ?? "3000";
const apiAddress = process.env.TACIT_API_ADDRESS ?? "127.0.0.1:8080";

function reachableHost(host) {
  return host === "0.0.0.0" || host === "::" ? "127.0.0.1" : host;
}

function reachableAddress(address) {
  if (address.startsWith(":")) return `127.0.0.1${address}`;
  if (address.startsWith("0.0.0.0:")) return address.replace("0.0.0.0:", "127.0.0.1:");
  if (address.startsWith("[::]:")) return address.replace("[::]:", "127.0.0.1:");
  return address;
}

const webURL = `http://${reachableHost(webHost)}:${webPort}/healthz`;
const apiHealthURL = process.env.TACIT_API_HEALTH_URL ?? `http://${reachableAddress(apiAddress)}/healthz`;

const services = [
  {
    args: ["run", "./cmd/api", "--listen", apiAddress],
    command: "go",
    name: "api",
  },
  {
    args: ["--filter", "@tacit/web", "dev", "--hostname", webHost, "--port", webPort],
    command: "pnpm",
    env: {
      ...process.env,
      TACIT_API_ORIGIN: process.env.TACIT_API_ORIGIN ?? `http://${reachableAddress(apiAddress)}`,
      TACIT_WEB_ORIGIN: process.env.TACIT_WEB_ORIGIN ?? `http://${reachableHost(webHost)}:${webPort}`,
    },
    name: "web",
  },
];

const children = new Map();
let shuttingDown = false;
let exitCode = 0;

function terminateProcessTree(child, signal) {
  if (child.pid === undefined) return;

  try {
    if (process.platform === "win32") child.kill(signal);
    else process.kill(-child.pid, signal);
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
  }
}

function shutdown(signal = "SIGTERM", requestedExitCode = exitCode) {
  if (shuttingDown) return;
  shuttingDown = true;
  exitCode = requestedExitCode;

  for (const child of children.values()) terminateProcessTree(child, signal);
  if (children.size === 0) process.exit(exitCode);
}

function startService(service) {
  const child = spawn(service.command, service.args, {
    detached: process.platform !== "win32",
    env: service.env ?? process.env,
    stdio: "inherit",
  });
  children.set(service.name, child);

  child.once("error", (error) => {
    console.error(`[dev] Could not start ${service.name}: ${error.message}`);
    shutdown("SIGTERM", 1);
  });

  child.once("exit", (code, signal) => {
    children.delete(service.name);

    if (!shuttingDown) {
      console.error(`[dev] ${service.name} exited unexpectedly (${signal ?? `code ${code ?? 1}`}).`);
      shutdown("SIGTERM", code === 0 ? 1 : (code ?? 1));
    }

    if (children.size === 0) process.exit(exitCode);
  });
}

async function waitForEndpoint(name, url) {
  const deadline = Date.now() + 60_000;
  while (!shuttingDown && Date.now() < deadline) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1_000) });
      if (response.ok) {
        console.log(`[dev] ${name} ready: ${url}`);
        return true;
      }
    } catch {
      // The process is still starting. Retry until the shared deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  if (!shuttingDown) {
    console.error(`[dev] ${name} did not become ready within 60 seconds: ${url}`);
    shutdown("SIGTERM", 1);
  }
  return false;
}

console.log(`[dev] Web: ${webURL}`);
console.log(`[dev] API: ${apiHealthURL}`);
console.log("[dev] Press Ctrl+C to stop all services.");

process.once("SIGINT", () => shutdown("SIGINT", 0));
process.once("SIGTERM", () => shutdown("SIGTERM", 0));

for (const service of services) startService(service);

const readiness = await Promise.all([
  waitForEndpoint("API", apiHealthURL),
  waitForEndpoint("Web", webURL),
]);
if (readiness.every(Boolean) && !shuttingDown) console.log("[dev] Tacit is ready.");
