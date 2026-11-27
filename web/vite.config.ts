import { writeFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

const outDir = fileURLToPath(new URL("../internal/webui/dist", import.meta.url));

// The build is embedded into agentd; .gitkeep keeps the directory in git between builds.
const keepDir: Plugin = {
  name: "charter-keep-dist",
  closeBundle() {
    writeFileSync(`${outDir}/.gitkeep`, "");
  },
};

export default defineConfig({
  // Relative asset paths: the core serves the UI under /ui/ and a proxy may add its own prefix.
  base: "./",
  plugins: [react(), tailwindcss(), keepDir],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: { outDir, emptyOutDir: true },
  server: {
    // In development the API goes to a local agentd.
    proxy: { "/v1": process.env.CHARTER_DEV_API ?? "http://localhost:8080" },
  },
});
