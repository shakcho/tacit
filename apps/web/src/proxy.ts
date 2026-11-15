import { authkit, handleAuthkitProxy } from "@workos-inc/authkit-nextjs";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

const protectedPrefixes = [
  "/design-system",
  "/help",
  "/highlights",
  "/meetings",
  "/playlists",
  "/profile",
  "/projects",
  "/search",
] as const;

function isProductPath(pathname: string): boolean {
  return protectedPrefixes.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`));
}

function requiresIdentity(pathname: string): boolean {
  return isProductPath(pathname) || pathname.startsWith("/api/") || [
    "/auth/callback",
    "/auth/sign-in",
    "/auth/sign-out",
    "/auth/sign-up",
    "/auth/switch-organization",
  ].includes(pathname);
}

function hasWorkOSConfiguration(): boolean {
  return [
    process.env.WORKOS_CLIENT_ID,
    process.env.WORKOS_COOKIE_PASSWORD,
    process.env.WORKOS_ISSUER,
    process.env.NEXT_PUBLIC_WORKOS_REDIRECT_URI,
  ].every((value) => value !== undefined && value.length > 0);
}

export default async function proxy(request: NextRequest) {
  if (!hasWorkOSConfiguration()) {
    if (process.env.NODE_ENV === "production" && requiresIdentity(request.nextUrl.pathname)) {
      return NextResponse.json({ status: 503, code: "IDENTITY_NOT_CONFIGURED" }, { status: 503 });
    }
    if (isProductPath(request.nextUrl.pathname)) {
      return NextResponse.redirect(new URL("/auth/setup", request.url));
    }
    return NextResponse.next();
  }

  const result = await authkit(request);
  if (isProductPath(request.nextUrl.pathname) && !result.session.user && result.authorizationUrl) {
    return handleAuthkitProxy(request, result.headers, { redirect: result.authorizationUrl });
  }
  return handleAuthkitProxy(request, result.headers);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|icon.svg).*)"],
};
