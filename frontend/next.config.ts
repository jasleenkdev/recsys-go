import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Pin the workspace root to this directory. Without it Turbopack walks
  // up and picks a stray package-lock.json outside the repo.
  turbopack: { root: __dirname },
};

export default nextConfig;
