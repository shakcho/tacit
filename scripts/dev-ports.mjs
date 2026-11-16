import { spawnSync } from "node:child_process";
import process from "node:process";

const defaultGracePeriodMs = 2_000;
const defaultPollIntervalMs = 100;

export function normalizePort(value, label = "port") {
  const source = String(value);
  if (!/^\d+$/.test(source)) throw new Error(`${label} must be a number, received "${source}"`);

  const port = Number(source);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) {
    throw new Error(`${label} must be between 1 and 65535, received "${source}"`);
  }
  return String(port);
}

export function portFromAddress(address) {
  const match = /:(\d+)$/.exec(address);
  if (match === null) throw new Error(`API address must end with a port, received "${address}"`);
  return normalizePort(match[1], "API port");
}

export function parseProcessIDs(output) {
  return [...new Set(output
    .split(/\s+/)
    .filter(Boolean)
    .map(Number)
    .filter((pid) => Number.isInteger(pid) && pid > 0))];
}

export function findListeningProcessIDs(ports, run = spawnSync) {
  const normalizedPorts = [...new Set(ports.map((port) => normalizePort(port)))];
  const result = run(
    "lsof",
    ["-t", ...normalizedPorts.map((port) => `-iTCP:${port}`), "-sTCP:LISTEN"],
    { encoding: "utf8" },
  );

  if (result.error !== undefined) {
    if (result.error.code === "ENOENT") {
      throw new Error("lsof is required to free Tacit's development ports before startup");
    }
    throw result.error;
  }
  if (result.status === 1) return [];
  if (result.status !== 0) {
    const detail = result.stderr?.trim();
    throw new Error(`lsof failed while checking development ports${detail ? `: ${detail}` : ""}`);
  }
  return parseProcessIDs(result.stdout ?? "");
}

function signalProcess(pid, signal, killProcess) {
  try {
    killProcess(pid, signal);
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
  }
}

async function waitForRelease(findProcessIDs, timeoutMs, pollIntervalMs, sleep) {
  let remaining = findProcessIDs();
  const attempts = Math.max(1, Math.ceil(timeoutMs / pollIntervalMs));

  for (let attempt = 0; remaining.length > 0 && attempt < attempts; attempt += 1) {
    await sleep(pollIntervalMs);
    remaining = findProcessIDs();
  }
  return remaining;
}

export async function releaseDevelopmentPorts(ports, options = {}) {
  const normalizedPorts = [...new Set(ports.map((port) => normalizePort(port)))];
  const findProcessIDs = options.findProcessIDs ?? (() => findListeningProcessIDs(normalizedPorts));
  const killProcess = options.killProcess ?? process.kill.bind(process);
  const logger = options.logger ?? console;
  const pollIntervalMs = options.pollIntervalMs ?? defaultPollIntervalMs;
  const gracePeriodMs = options.gracePeriodMs ?? defaultGracePeriodMs;
  const forcePeriodMs = options.forcePeriodMs ?? defaultGracePeriodMs;
  const sleep = options.sleep ?? ((milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)));
  const activeProcessIDs = () => findProcessIDs().filter((pid) => pid !== process.pid);

  let remaining = activeProcessIDs();
  if (remaining.length === 0) return;

  logger.log(`[dev] Stopping existing listeners on ports ${normalizedPorts.join(", ")}: ${remaining.join(", ")}.`);
  for (const pid of remaining) signalProcess(pid, "SIGTERM", killProcess);

  remaining = await waitForRelease(activeProcessIDs, gracePeriodMs, pollIntervalMs, sleep);
  if (remaining.length > 0) {
    logger.warn(`[dev] Force-stopping listeners that did not exit: ${remaining.join(", ")}.`);
    for (const pid of remaining) signalProcess(pid, "SIGKILL", killProcess);
    remaining = await waitForRelease(activeProcessIDs, forcePeriodMs, pollIntervalMs, sleep);
  }

  if (remaining.length > 0) {
    throw new Error(`ports ${normalizedPorts.join(", ")} are still occupied by processes ${remaining.join(", ")}`);
  }
}
