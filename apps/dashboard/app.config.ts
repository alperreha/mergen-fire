import { defineConfig } from "@solidjs/start/config";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  vite: {
    plugins: [tailwindcss()],
    server: {
      proxy: {
        "/v1": "http://127.0.0.1:8080",
      },
    },
  }
});
