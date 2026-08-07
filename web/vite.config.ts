import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5281,
    proxy: {
      "/api": {
        target: "http://localhost:8181",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
  },
})
