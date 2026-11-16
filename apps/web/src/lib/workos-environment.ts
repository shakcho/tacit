type Environment = Readonly<Record<string, string | undefined>>;

const requiredWorkOSVariables = [
  "WORKOS_CLIENT_ID",
  "WORKOS_COOKIE_PASSWORD",
  "WORKOS_ISSUER",
  "NEXT_PUBLIC_WORKOS_REDIRECT_URI",
] as const;

const loopbackHostnames = new Set(["127.0.0.1", "[::1]", "localhost"]);

export function hasWorkOSConfiguration(environment: Environment = process.env): boolean {
  return requiredWorkOSVariables.every((name) => {
    const value = environment[name];
    return value !== undefined && value.length > 0;
  });
}

type CanonicalLoopbackURLInput = {
  environment?: Environment;
  requestHost?: string | null;
  requestURL: URL;
};

export function getCanonicalLoopbackURL({
  environment = process.env,
  requestHost,
  requestURL,
}: CanonicalLoopbackURLInput): URL | undefined {
  const redirectURI = environment.NEXT_PUBLIC_WORKOS_REDIRECT_URI;
  if (redirectURI === undefined) return undefined;

  let callbackURL: URL;
  try {
    callbackURL = new URL(redirectURI);
  } catch {
    return undefined;
  }

  let browserURL = requestURL;
  if (requestHost !== undefined && requestHost !== null) {
    try {
      browserURL = new URL(`${requestURL.protocol}//${requestHost}`);
    } catch {
      return undefined;
    }
  }

  if (!loopbackHostnames.has(browserURL.hostname) || !loopbackHostnames.has(callbackURL.hostname)) return undefined;
  if (browserURL.origin === callbackURL.origin) return undefined;

  const canonicalURL = new URL(`${requestURL.pathname}${requestURL.search}`, callbackURL.origin);
  return canonicalURL;
}
