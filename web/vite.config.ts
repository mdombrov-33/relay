import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const api = {
  "/v1": { target: "http://127.0.0.1:4000", changeOrigin: true },
};

export default defineConfig({
  plugins: [react()],
  server: { proxy: api },
  preview: { proxy: api },
});
