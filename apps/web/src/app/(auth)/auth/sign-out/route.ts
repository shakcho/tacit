import { signOut, withAuth } from "@workos-inc/authkit-nextjs";
import type { NextRequest } from "next/server";

import { hasExpectedOrigin } from "@/auth/api-proxy-policy";
import { getServerEnvironment } from "@/env/server";

function failure(status: number, code: string): Response {
  return Response.json({ code, status }, {
    headers: { "Cache-Control": "no-store", "Content-Type": "application/problem+json" },
    status,
  });
}

export async function POST(request: NextRequest): Promise<Response> {
  const environment = getServerEnvironment();
  if (!hasExpectedOrigin(request, environment.webOrigin.origin)) return failure(403, "CROSS_SITE_REQUEST");

  const auth = await withAuth();
  if (auth.user && auth.accessToken !== undefined) {
    let denial: Response;
    try {
      denial = await fetch(new URL("/internal/auth/session-denials", environment.apiOrigin), {
        cache: "no-store",
        headers: { Authorization: `Bearer ${auth.accessToken}` },
        method: "POST",
        redirect: "manual",
        signal: AbortSignal.timeout(10_000),
      });
    } catch {
      return failure(503, "DENIAL_UNAVAILABLE");
    }
    if (!denial.ok) return failure(denial.status >= 500 ? 503 : 401, "DENIAL_REJECTED");
  }

  await signOut({ returnTo: environment.webOrigin.origin });
  return new Response(null, { status: 204 });
}
