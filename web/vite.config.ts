import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { federation } from "@module-federation/vite";

// labor_mfe: the labor-performance remote. Exposes ./App -- the shell
// lazy-loads it at /labor/*. Also runnable standalone on :5187 for local
// development without the shell (see main.tsx). Port 5187 is the one
// labor-performance's own README already reserves in its
// CORS_ALLOWED_ORIGINS default, pre-dating this remote's existence.
export default defineConfig({
  plugins: [
    react(),
    federation({
      name: "labor_mfe",
      filename: "remoteEntry.js",
      exposes: {
        "./App": "./src/App.tsx",
      },
      shared: {
        react: { singleton: true, requiredVersion: "^19.2.8" },
        "react-dom": { singleton: true, requiredVersion: "^19.2.8" },
        "react-router-dom": { singleton: true, requiredVersion: "^7.18.3" },
        "@warehouse/ui-kit": { singleton: true },
      },
    }),
  ],
  server: {
    port: 5187,
    strictPort: true,
    cors: true,
    origin: "http://localhost:5187",
  },
  preview: {
    port: 5187,
    strictPort: true,
    cors: true,
  },
  build: {
    target: "esnext",
    modulePreload: false,
  },
});
