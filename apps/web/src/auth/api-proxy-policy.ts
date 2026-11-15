const safeMethods = new Set(["GET", "HEAD"]);

const allowedRoutes = [
  { method: "GET", pattern: /^\/v1\/auth\/session$/u },
  { method: "GET", pattern: /^\/v1\/shell-samples\/smp_[0-9a-f]{32}$/u },
  { method: "POST", pattern: /^\/v1\/shell-samples$/u },
] as const;

export const maximumProxyBodyBytes = 1 << 20;

export function backendPath(segments: readonly string[]): string | null {
  if (segments.length === 0) return null;
  for (const segment of segments) {
    if (segment.length === 0 || segment === "." || segment === ".." || segment.includes("/") || segment.includes("\\")) {
      return null;
    }
  }
  return `/${segments.join("/")}`;
}

export function isAllowedBackendRequest(method: string, path: string): boolean {
  return allowedRoutes.some((route) => route.method === method && route.pattern.test(path));
}

export function isSafeMethod(method: string): boolean {
  return safeMethods.has(method);
}

export function hasExpectedOrigin(request: Request, expectedOrigin: string): boolean {
  if (isSafeMethod(request.method)) return true;
  const origin = request.headers.get("Origin");
  const fetchSite = request.headers.get("Sec-Fetch-Site");
  return origin === expectedOrigin && (fetchSite === null || fetchSite === "same-origin");
}

export function upstreamRequestHeaders(request: Request, accessToken: string): Headers {
  const headers = new Headers({
    Accept: "application/json, application/problem+json",
    Authorization: `Bearer ${accessToken}`,
  });
  for (const name of ["Content-Type", "Idempotency-Key", "X-Request-ID", "X-Tacit-Organization"]) {
    const value = request.headers.get(name);
    if (value !== null) headers.set(name, value);
  }
  return headers;
}

export function downstreamResponseHeaders(upstream: Headers): Headers {
  const headers = new Headers({ "Cache-Control": "no-store" });
  for (const name of ["Content-Type", "Location", "Retry-After", "X-Request-ID"]) {
    const value = upstream.get(name);
    if (value !== null) headers.set(name, value);
  }
  return headers;
}
