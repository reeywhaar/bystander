import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Four entries rather than one SPA with routes.
//
// These are four applications with four audiences. The login shell is the only document an
// unauthenticated visitor receives, so the reader's code is never shipped to somebody who
// has not signed in; the admin bundle is not merely hidden from a subscriber but never
// sent to them. See docs/frontend.md.
//
// Naming all four here means a missing one is a hard error rather than a quietly smaller
// build — and the Dockerfile asserts each is non-empty afterwards.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  // `@app/lib/time` rather than `../../lib/time`. An import that names where a module *is*
  // stops depending on where the importer sits, so moving a file is moving a file.
  //
  // Root-relative rather than resolved through `node:url`: this project has no
  // `@types/node`, and Vite reads a leading slash as "from the project root" anyway.
  resolve: { alias: { "@app": "/src" } },
  build: {
    outDir: "dist",
    // NOT emptyOutDir. `dist/.gitkeep` is tracked so the directory exists in a fresh
    // clone, which is what lets `go run .` serve its placeholder instead of erroring on a
    // path that is not there. It used to matter far more: the bundle was compiled into the
    // binary with //go:embed, and an absent dist failed the Go build outright.
    //
    // Nothing accumulates as a result: the HTML entries are overwritten by name, and
    // `npm run build` removes dist/assets and dist/landing first — the two directories whose
    // contents are named by something other than this config. assets is content-hashed;
    // landing is whatever the screenshot capture last wrote, and it had three PNGs in it from
    // back when the landing shots were PNGs, embedded into every binary built since.
    emptyOutDir: false,
    rollupOptions: {
      input: {
        index: "index.html",
        login: "login.html",
        manage: "manage.html",
        admin: "admin.html",
        public: "public.html",
      },
    },
  },
  server: {
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
});
