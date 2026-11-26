import type { AuthenticationSession } from "@/api/generated/models";
import { parseAuthenticationSession } from "@/auth/session-contract";
import { getServerEnvironment } from "@/env/server";

export type LocalAdmission =
  | { kind: "admitted"; session: AuthenticationSession }
  | { kind: "not-admitted" }
  | { kind: "unavailable" };

export async function localAdmission(accessToken: string | undefined): Promise<LocalAdmission> {
  if (accessToken === undefined) return { kind: "not-admitted" };

  let response: Response;
  try {
    response = await fetch(new URL("/v1/auth/session", getServerEnvironment().apiOrigin), {
      cache: "no-store",
      headers: { Authorization: `Bearer ${accessToken}` },
      redirect: "manual",
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return { kind: "unavailable" };
  }
  if (response.status === 401 || response.status === 403) return { kind: "not-admitted" };
  if (!response.ok) return { kind: "unavailable" };
  try {
    const session = parseAuthenticationSession(await response.json());
    return session === undefined ? { kind: "unavailable" } : { kind: "admitted", session };
  } catch {
    return { kind: "unavailable" };
  }
}
