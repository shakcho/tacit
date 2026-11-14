export type ServerEnvironment = Readonly<{
  apiOrigin: URL;
}>;

function parseServerURL(input: string | undefined): URL {
  if (input === undefined || input.length === 0) return new URL("http://127.0.0.1:8080");

  const value = new URL(input);
  if (value.protocol !== "http:" && value.protocol !== "https:") {
    throw new Error("TACIT_API_ORIGIN must use http or https");
  }
  return value;
}

export function getServerEnvironment(): ServerEnvironment {
  return Object.freeze({
    apiOrigin: parseServerURL(process.env.TACIT_API_ORIGIN),
  });
}
