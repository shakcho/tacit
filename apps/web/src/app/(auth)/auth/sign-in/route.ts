import { getSignInUrl } from "@workos-inc/authkit-nextjs";
import { NextResponse } from "next/server";

import { getServerEnvironment } from "@/env/server";

export async function GET(): Promise<Response> {
  const { workOSOrganizationID } = getServerEnvironment();
  const organization = workOSOrganizationID === undefined ? {} : { organizationId: workOSOrganizationID };
  return NextResponse.redirect(await getSignInUrl({ ...organization, returnTo: "/meetings" }));
}
