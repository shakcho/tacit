export type ServerEnvironment = Readonly<{
  apiOrigin: URL;
  webOrigin: URL;
}>;

function parseServerURL(input: string | undefined, fallback: string, name: string): URL {
  if (input === undefined || input.length === 0) return new URL(fallback);

  const value = new URL(input);
  if (value.protocol !== "http:" && value.protocol !== "https:") {
    throw new Error(`${name} must use http or https`);
  }
  if (value.pathname !== "/" || value.search.length > 0 || value.hash.length > 0) {
    throw new Error(`${name} must be an origin without a path, query, or fragment`);
  }
  return value;
}

export function getServerEnvironment(): ServerEnvironment {
  return Object.freeze({
    apiOrigin: parseServerURL(process.env.TACIT_API_ORIGIN, "http://127.0.0.1:8080", "TACIT_API_ORIGIN"),
    webOrigin: parseServerURL(process.env.TACIT_WEB_ORIGIN, "http://127.0.0.1:3000", "TACIT_WEB_ORIGIN"),
  });
}
