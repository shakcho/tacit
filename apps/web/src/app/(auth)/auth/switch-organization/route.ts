import { switchToOrganization, withAuth } from "@workos-inc/authkit-nextjs";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { hasExpectedOrigin } from "@/auth/api-proxy-policy";
import { isWorkOSOrganizationID } from "@/auth/workos-organization";
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

  const form = await request.formData();
  const organizationID = form.get("organizationId");
  if (typeof organizationID !== "string" || !isWorkOSOrganizationID(organizationID)) {
    return failure(422, "INVALID_ORGANIZATION");
  }

  await withAuth({ ensureSignedIn: true });
  const switched = await switchToOrganization(organizationID, { revalidationStrategy: "none" });
  const admission = await fetch(new URL("/v1/auth/session", environment.apiOrigin), {
    cache: "no-store",
    headers: { Authorization: `Bearer ${switched.accessToken}` },
    redirect: "manual",
    signal: AbortSignal.timeout(10_000),
  });
  if (!admission.ok) return failure(admission.status >= 500 ? 503 : 403, "ORGANIZATION_NOT_ADMITTED");

  return NextResponse.redirect(new URL("/meetings", environment.webOrigin), 303);
}
