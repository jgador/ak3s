import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { liveClusterPlugin } from "./server/live.mjs";

export default defineConfig({
  plugins: [react(), liveClusterPlugin()],
  envDir: false,
  base: "./",
  server: { host: "127.0.0.1", port: 5173, strictPort: true, cors: false },
  preview: { host: "127.0.0.1", port: 4173, strictPort: true, cors: false },
});
