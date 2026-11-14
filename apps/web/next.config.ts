import type { NextConfig } from "next";
import { resolve } from "node:path";

import { publicEnvironment } from "./src/env/public";
import { getServerEnvironment } from "./src/env/server";

const nextConfig = {
  agentRules: false,
  output: "standalone",
  outputFileTracingRoot: resolve(import.meta.dirname, "../.."),
  poweredByHeader: false,
  reactStrictMode: true,
  rewrites() {
    const { apiOrigin } = getServerEnvironment();
    return [{
      destination: `${apiOrigin.origin}/:path*`,
      source: `${publicEnvironment.apiBasePath}/:path*`,
    }];
  },
  typedRoutes: true,
} satisfies NextConfig;

export default nextConfig;
