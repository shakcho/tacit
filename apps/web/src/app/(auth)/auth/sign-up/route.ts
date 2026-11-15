import { getSignUpUrl } from "@workos-inc/authkit-nextjs";
import { NextResponse } from "next/server";

export async function GET(): Promise<Response> {
  return NextResponse.redirect(await getSignUpUrl({ returnTo: "/meetings" }));
}
