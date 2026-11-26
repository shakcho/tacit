import { getServerEnvironment } from "@/env/server";
import { hasWorkOSConfiguration } from "@/lib/workos-environment";

async function isReady(): Promise<boolean> {
  try {
    const { apiOrigin, webOrigin } = getServerEnvironment();
    const environment = process.env;
    const configured = hasWorkOSConfiguration(environment)
      && (environment.WORKOS_COOKIE_PASSWORD?.length ?? 0) >= 32
      && environment.NEXT_PUBLIC_WORKOS_REDIRECT_URI === new URL("/auth/callback", webOrigin).href
      && webOrigin.protocol === "https:"
      && new URL(environment.WORKOS_ISSUER ?? "").protocol === "https:";
    if (environment.NODE_ENV === "production" && !configured) return false;
    const upstream = await fetch(new URL("/readyz", apiOrigin), {
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(1_500),
    });
    const ready = upstream.ok;
    void upstream.body?.cancel().catch(() => undefined);
    return ready;
  } catch {
    return false;
  }
}

export async function GET(): Promise<Response> {
  const ready = await isReady();
  return Response.json({ status: ready ? "ok" : "unavailable" }, {
    headers: { "Cache-Control": "no-store" },
    status: ready ? 200 : 503,
  });
}
