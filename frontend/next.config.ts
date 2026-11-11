import type { NextConfig } from "next";

// Static export. Go serves out/ next to /api (see FRONTEND_DIR in
// web-go/backend/app), so the STB runs the site without a Node process.
// cost: no server rendering, so every page resolves its data in the browser
// against the same origin that serves the HTML.
const nextConfig: NextConfig = {
  output: "export",
  // No image optimizer exists in a static export; next/image must emit <img>.
  images: { unoptimized: true },
};

export default nextConfig;