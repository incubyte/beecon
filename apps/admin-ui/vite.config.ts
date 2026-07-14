import { fileURLToPath, URL } from "node:url";

import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Admin UI is a pure client-side SPA (PD47/§2.1): TanStack Start's SSR
// and server-functions layer is deliberately not used — the Go binary is
// the only server, so this config is plain Vite + the TanStack Router file-
// based-routing plugin, wired the same way TanStack Start wires it
// internally when its own SSR is switched off. `base: '/admin/'` matches
// the embedded mount so every built asset URL resolves under it, and
// `build.outDir` points at the FD2 embed target so a plain `vite build`
// (run by the top-level `build-ui` task, before `go build`) leaves the
// bundle exactly where `//go:embed dist` expects it.
export default defineConfig({
  base: "/admin/",
  plugins: [
    tanstackRouter({
      target: "react",
      autoCodeSplitting: true,
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
    }),
    react(),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    outDir: "../../server/internal/adminui/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/admin/verify": "http://localhost:8080",
    },
  },
});
