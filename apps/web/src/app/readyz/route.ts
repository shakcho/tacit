import { getServerEnvironment } from "@/env/server";
import { hasWorkOSConfiguration } from "@/lib/workos-environment";

export async function GET(): Promise<Response> {
  let ready = false;
  try {
    const { apiOrigin, webOrigin } = getServerEnvironment();
    const environment = process.env;
    const configured = hasWorkOSConfiguration(environment)
      && Boolean(environment.WORKOS_API_KEY)
      && (environment.WORKOS_COOKIE_PASSWORD?.length ?? 0) >= 32
      && environment.NEXT_PUBLIC_WORKOS_REDIRECT_URI === new URL("/auth/callback", webOrigin).href
      && webOrigin.protocol === "https:"
      && new URL(environment.WORKOS_ISSUER ?? "").protocol === "https:";
    if (environment.NODE_ENV !== "production" || configured) {
      const upstream = await fetch(new URL("/readyz", apiOrigin), {
        cache: "no-store",
        redirect: "error",
        signal: AbortSignal.timeout(1_500),
      });
      ready = upstream.ok;
      void upstream.body?.cancel().catch(() => undefined);
    }
  } catch {
    // A malformed installation or an unavailable API cannot take traffic.
  }
  return Response.json({ status: ready ? "ok" : "unavailable" }, {
    headers: { "Cache-Control": "no-store" },
    status: ready ? 200 : 503,
  });
}
