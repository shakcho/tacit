const defaultAPIBasePath = "/api";

export type PublicEnvironment = Readonly<{
  apiBasePath: string;
}>;

function parsePublicPath(input: string | undefined): string {
  if (input === undefined || input.length === 0) return defaultAPIBasePath;
  if (!input.startsWith("/") || input.startsWith("//") || (input.length > 1 && input.endsWith("/"))) {
    throw new Error("NEXT_PUBLIC_API_BASE_PATH must be a root-relative path without a trailing slash");
  }
  return input;
}

export const publicEnvironment: PublicEnvironment = Object.freeze({
  apiBasePath: parsePublicPath(process.env.NEXT_PUBLIC_API_BASE_PATH),
});
