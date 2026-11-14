import type { NextConfig } from "next";
import { resolve } from "node:path";

const nextConfig = {
  output: "standalone",
  outputFileTracingRoot: resolve(import.meta.dirname, "../.."),
  poweredByHeader: false,
  reactStrictMode: true,
  typedRoutes: true,
} satisfies NextConfig;

export default nextConfig;
