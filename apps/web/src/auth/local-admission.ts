import { getServerEnvironment } from "@/env/server";

export type LocalAdmission = "admitted" | "not-admitted" | "unavailable";

export async function localAdmission(accessToken: string | undefined): Promise<LocalAdmission> {
  if (accessToken === undefined) return "not-admitted";

  let response: Response;
  try {
    response = await fetch(new URL("/v1/auth/session", getServerEnvironment().apiOrigin), {
      cache: "no-store",
      headers: { Authorization: `Bearer ${accessToken}` },
      redirect: "manual",
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return "unavailable";
  }
  if (response.ok) return "admitted";
  if (response.status === 401 || response.status === 403) return "not-admitted";
  return "unavailable";
}
