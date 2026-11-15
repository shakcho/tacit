type Environment = Readonly<Record<string, string | undefined>>;

const requiredWorkOSVariables = [
  "WORKOS_CLIENT_ID",
  "WORKOS_COOKIE_PASSWORD",
  "WORKOS_ISSUER",
  "NEXT_PUBLIC_WORKOS_REDIRECT_URI",
] as const;

export function hasWorkOSConfiguration(environment: Environment = process.env): boolean {
  return requiredWorkOSVariables.every((name) => {
    const value = environment[name];
    return value !== undefined && value.length > 0;
  });
}
