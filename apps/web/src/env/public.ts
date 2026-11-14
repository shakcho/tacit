const defaultGraphQLPath = "/graphql";

export type PublicEnvironment = Readonly<{
  graphQLPath: string;
}>;

function parsePublicPath(input: string | undefined): string {
  if (input === undefined || input.length === 0) return defaultGraphQLPath;
  if (!input.startsWith("/")) throw new Error("NEXT_PUBLIC_GRAPHQL_PATH must be a root-relative path");
  return input;
}

export const publicEnvironment: PublicEnvironment = Object.freeze({
  graphQLPath: parsePublicPath(process.env.NEXT_PUBLIC_GRAPHQL_PATH),
});
