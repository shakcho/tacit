import { defineConfig } from "orval";

export default defineConfig({
  tacit: {
    input: {
      filters: { mode: "exclude", tags: ["Realtime"] },
      target: "../../internal/adapters/httpapi/openapi.yaml",
    },
    output: {
      baseUrl: {
        imports: [{ importPath: "../../env/public", name: "publicEnvironment" }],
        runtime: "publicEnvironment.apiBasePath",
      },
      clean: ["./src/api/generated"],
      client: "react-query",
      httpClient: "fetch",
      override: {
        fetch: {
          includeHttpResponseReturnType: false,
        },
        mutator: {
          name: "apiFetch",
          path: "./src/api/client.ts",
        },
      },
      schemas: "./src/api/generated/models",
      target: "./src/api/generated/tacit.ts",
    },
  },
});
