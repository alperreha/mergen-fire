import { paraglideVitePlugin } from "@inlang/paraglide-js";
import { defineConfig } from "@solidjs/start/config";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  // e.g. https://github.com/solidjs/solid-start/pull/1844
  // NOTE: ssr:true causes sessionStorage not defined error
  ssr: false,
  solid: {
    babel: {
      plugins: [
        [
          "@locator/babel-jsx/dist",
          {
            env: "development",
          },
        ],
      ],
    },
  },

  // CSP and CSRF should be solved in load balancer side not in frontend code.
  // follow: https://docs.solidjs.com/solid-start/guides/security
  // middleware: "src/middlewares/index.ts",
  vite: {
    plugins: [
      tailwindcss() as any,
      // e.g. https://github.com/opral/monorepo/tree/main/inlang/packages/paraglide/paraglide-js/examples/vite
      // https://inlang.com/m/gerre34r/library-inlang-paraglideJs/strategy#translated-pathnames
      paraglideVitePlugin({
        project: "./project.inlang",
        outdir: "./src/paraglide",
        // forcing locale modules to detect problems during CI/CD
        // (all other projects use message-modules)
        outputStructure: "locale-modules",
      }),
    ],
    server: {
      // e.g. https://vite.dev/config/server-options.html#server-proxy
      proxy: {
        "/v1": "http://localhost:8080",
      },
    },
  },
});
