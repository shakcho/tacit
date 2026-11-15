import { withAuth } from "@workos-inc/authkit-nextjs";
import type { NextRequest } from "next/server";

import {
  backendPath,
  downstreamResponseHeaders,
  hasExpectedOrigin,
  isAllowedBackendRequest,
  isSafeMethod,
  maximumProxyBodyBytes,
  upstreamRequestHeaders,
} from "@/auth/api-proxy-policy";
import { getServerEnvironment } from "@/env/server";

type RouteContext = Readonly<{ params: Promise<{ path: string[] }> }>;

function problem(status: number, code: string, detail: string): Response {
  return Response.json({
    code,
    detail,
    status,
    title: status === 401 ? "Authentication required" : "Request rejected",
    type: `https://api.tacit.dev/problems/${code.toLowerCase().replaceAll("_", "-")}`,
  }, {
    headers: { "Cache-Control": "no-store", "Content-Type": "application/problem+json" },
    status,
  });
}

async function requestBody(request: Request): Promise<ArrayBuffer | undefined> {
  if (isSafeMethod(request.method)) return undefined;
  const declaredLength = Number(request.headers.get("Content-Length") ?? "0");
  if (Number.isFinite(declaredLength) && declaredLength > maximumProxyBodyBytes) {
    throw new RangeError("request body is too large");
  }
  const body = await request.arrayBuffer();
  if (body.byteLength > maximumProxyBodyBytes) throw new RangeError("request body is too large");
  return body;
}

async function forward(request: NextRequest, context: RouteContext): Promise<Response> {
  if (request.headers.has("Authorization")) {
    return problem(400, "BROWSER_CREDENTIAL_REJECTED", "The web boundary supplies its own API credential.");
  }

  const environment = getServerEnvironment();
  if (!hasExpectedOrigin(request, environment.webOrigin.origin)) {
    return problem(403, "CROSS_SITE_REQUEST", "The request origin is not allowed.");
  }

  const path = backendPath((await context.params).path);
  if (path === null || !isAllowedBackendRequest(request.method, path)) {
    return problem(404, "NOT_FOUND", "The requested API operation is not exposed by the web boundary.");
  }

  const auth = await withAuth();
  if (!auth.user || auth.accessToken === undefined) {
    return problem(401, "UNAUTHENTICATED", "Sign in before calling the Tacit API.");
  }

  let body: ArrayBuffer | undefined;
  try {
    body = await requestBody(request);
  } catch (error) {
    if (error instanceof RangeError) return problem(413, "REQUEST_TOO_LARGE", "The request exceeds the one MiB limit.");
    throw error;
  }

  const target = new URL(path, environment.apiOrigin);
  target.search = request.nextUrl.search;
  let upstream: Response;
  try {
    upstream = await fetch(target, {
      body,
      cache: "no-store",
      headers: upstreamRequestHeaders(request, auth.accessToken),
      method: request.method,
      redirect: "manual",
      signal: AbortSignal.timeout(15_000),
    });
  } catch {
    return problem(503, "API_UNAVAILABLE", "The Tacit API is unavailable.");
  }
  if (upstream.status >= 300 && upstream.status < 400) {
    return problem(502, "UPSTREAM_REDIRECT", "The Tacit API returned an unexpected redirect.");
  }
  return new Response(upstream.body, {
    headers: downstreamResponseHeaders(upstream.headers),
    status: upstream.status,
  });
}

export const DELETE = forward;
export const GET = forward;
export const HEAD = forward;
export const PATCH = forward;
export const POST = forward;
export const PUT = forward;
