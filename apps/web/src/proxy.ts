import { authkit, handleAuthkitProxy } from "@workos-inc/authkit-nextjs";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { getCanonicalLoopbackURL, hasWorkOSConfiguration } from "@/lib/workos-environment";

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

function escapeHTMLAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function moveBrowserToCanonicalOrigin(destination: URL): Response {
  const url = destination.toString();
  const escapedURL = escapeHTMLAttribute(url);

  return new Response(
    `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${escapedURL}"><title>Opening Tacit</title></head><body><a href="${escapedURL}">Continue to Tacit</a></body></html>`,
    {
      headers: {
        "Cache-Control": "no-store",
        "Content-Type": "text/html; charset=utf-8",
        Refresh: `0;url=${url}`,
      },
      status: 200,
    },
  );
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

  const canonicalURL = getCanonicalLoopbackURL({
    requestHost: request.headers.get("host"),
    requestURL: new URL(request.url),
  });
  if (canonicalURL !== undefined) {
    // Next.js treats all loopback aliases as the same host and rewrites an
    // absolute redirect to a relative one. A refresh response keeps the
    // browser on the callback origin before AuthKit creates its PKCE cookie.
    return moveBrowserToCanonicalOrigin(canonicalURL);
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
