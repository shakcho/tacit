import type { QueryClient } from "@tanstack/react-query";

import { APIError } from "@/api/client";
import type { AuthenticationSession, RealtimeCursor, SessionRevision } from "@/api/generated/models";
import { getAuthenticationSession, getGetAuthenticationSessionQueryKey } from "@/api/generated/tacit";
import { eventsPath } from "@/auth/api-proxy-policy";
import { readSessionFrames, StreamProtocolError } from "@/auth/event-stream";
import { parseAuthenticationSession } from "@/auth/session-contract";
import { publicEnvironment } from "@/env/public";

type SubscriptionState =
  | { kind: "stream"; cursor: RealtimeCursor }
  | { kind: "refetchPending"; cursor: RealtimeCursor; revision: SessionRevision }
  | { kind: "snapshot" };

type AccessEnd = "revoked" | "scope_changed";

class AccessEnded extends Error {
  constructor(readonly reason: AccessEnd) {
    super(reason);
  }
}

type SessionSubscription = {
  client: QueryClient;
  initial: AuthenticationSession;
  signal: AbortSignal;
  onAccessEnded: (reason: AccessEnd) => void;
};

const sessionKey = getGetAuthenticationSessionQueryKey();
const initialRetryDelay = 250;
const maximumRetryDelay = 4_000;
const inactivityTimeout = 30_000;

export async function followSession(subscription: SessionSubscription): Promise<void> {
  const { client, initial, signal, onAccessEnded } = subscription;
  if (signal.aborted) return;
  const cached = client.getQueryData<AuthenticationSession | null>(sessionKey);
  const seed = cached && sameScope(initial, cached) && BigInt(cached.revision) >= BigInt(initial.revision) ? cached : initial;
  if (cached !== null && cached !== seed) client.setQueryData(sessionKey, seed);
  let state: SubscriptionState = cached === null ? { kind: "snapshot" } : { kind: "stream", cursor: seed.cursor };
  let retryDelay = initialRetryDelay;

  while (!signal.aborted) {
    try {
      if (state.kind === "snapshot") {
        const session = await refreshSession(subscription);
        state = { kind: "stream", cursor: session.cursor };
        retryDelay = initialRetryDelay;
        continue;
      }
      if (state.kind === "refetchPending") {
        await refreshSession(subscription, state.revision);
        state = { kind: "stream", cursor: state.cursor };
        retryDelay = initialRetryDelay;
        continue;
      }

      const connection = new AbortController();
      let idle = setTimeout(() => connection.abort(), inactivityTimeout);
      const onActivity = () => {
        clearTimeout(idle);
        idle = setTimeout(() => connection.abort(), inactivityTimeout);
      };
      try {
        const body = await openSessionStream(state.cursor, AbortSignal.any([signal, connection.signal]));
        for await (const frame of readSessionFrames(body, onActivity)) {
          signal.throwIfAborted();
          switch (frame.kind) {
            case "access_revoked":
              throw new AccessEnded("revoked");
            case "resync":
              throw new StreamProtocolError("Server requested a fresh snapshot");
            case "checkpoint":
              state = { kind: "stream", cursor: frame.cursor };
              break;
            case "invalidate": {
              if (frame.userId !== initial.userId) throw new StreamProtocolError("Event resource is outside the session");
              state = { kind: "refetchPending", cursor: frame.cursor, revision: frame.revision };
              const canonical = client.getQueryData<AuthenticationSession | null>(sessionKey);
              if (!canonical || BigInt(canonical.revision) < BigInt(frame.revision)) {
                await refreshSession(subscription, frame.revision);
              }
              state = { kind: "stream", cursor: frame.cursor };
              break;
            }
          }
          retryDelay = initialRetryDelay;
        }
      } finally {
        clearTimeout(idle);
        connection.abort();
      }
    } catch (error) {
      if (signal.aborted) return;
      if (error instanceof AccessEnded || (error instanceof APIError && isDenied(error.status))) {
        await purgeSession(subscription);
        if (!signal.aborted) onAccessEnded(error instanceof AccessEnded ? error.reason : "revoked");
        return;
      }
      if (error instanceof StreamProtocolError) {
        await purgeSession(subscription);
        state = { kind: "snapshot" };
      }
    }
    await waitForRetry(signal, retryDelay);
    retryDelay = Math.min(retryDelay * 2, maximumRetryDelay);
  }
}

async function openSessionStream(cursor: RealtimeCursor, signal: AbortSignal): Promise<ReadableStream<Uint8Array>> {
  const response = await fetch(`${publicEnvironment.apiBasePath}${eventsPath}`, {
    cache: "no-store",
    credentials: "same-origin",
    headers: { Accept: "text/event-stream", "Last-Event-ID": cursor },
    redirect: "error",
    signal,
  });
  if (!response.ok) {
    void response.body?.cancel().catch(() => undefined);
    if (response.status === 409) throw new StreamProtocolError("Resume cursor expired");
    if (isDenied(response.status)) throw new AccessEnded("revoked");
    throw new Error(`Cannot open event stream (${response.status})`);
  }
  const contentType = response.headers.get("Content-Type")?.split(";")[0]?.trim().toLowerCase();
  if (response.body === null || contentType !== "text/event-stream") {
    void response.body?.cancel().catch(() => undefined);
    throw new StreamProtocolError("Expected an event stream");
  }
  return response.body;
}

async function refreshSession(
  { client, initial, signal }: SessionSubscription,
  minimumRevision?: SessionRevision,
): Promise<AuthenticationSession> {
  await client.cancelQueries({ queryKey: sessionKey, exact: true });
  signal.throwIfAborted();
  return client.fetchQuery({
    queryKey: sessionKey,
    retry: false,
    staleTime: 0,
    queryFn: async ({ signal: querySignal }) => {
      const requestSignal = AbortSignal.any([signal, querySignal]);
      const session = parseAuthenticationSession(await getAuthenticationSession({ signal: requestSignal }));
      requestSignal.throwIfAborted();
      if (session === undefined) throw new Error("Invalid canonical session");
      if (!sameScope(session, initial)) {
        throw new AccessEnded("scope_changed");
      }
      if (minimumRevision !== undefined && BigInt(session.revision) < BigInt(minimumRevision)) {
        throw new Error("Canonical session has not reached the invalidation revision");
      }
      return session;
    },
  });
}

async function purgeSession({ client, signal }: SessionSubscription): Promise<void> {
  await client.cancelQueries();
  if (signal.aborted) return;
  client.setQueryData<AuthenticationSession | null>(sessionKey, null);
  const session = client.getQueryCache().find({ queryKey: sessionKey, exact: true });
  client.removeQueries({ predicate: (query) => query !== session });
  client.getMutationCache().clear();
}

function sameScope(left: AuthenticationSession, right: AuthenticationSession): boolean {
  return left.userId === right.userId && left.organizationId === right.organizationId;
}

function isDenied(status: number): boolean {
  return status === 401 || status === 403;
}

function waitForRetry(signal: AbortSignal, delay: number): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    };
    const timer = setTimeout(done, delay);
    signal.addEventListener("abort", done, { once: true });
  });
}
