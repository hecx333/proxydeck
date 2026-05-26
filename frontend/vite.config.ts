import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return;
          }
          if (id.includes("echarts") || id.includes("zrender") || id.includes("echarts-for-react")) {
            return "charts";
          }
          if (id.includes("antd") || id.includes("@ant-design") || id.includes("rc-")) {
            return "antd";
          }
          if (id.includes("react-router")) {
            return "router";
          }
          if (id.includes("@tanstack/react-query")) {
            return "query";
          }
          if (id.includes("axios")) {
            return "http";
          }
        }
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true
      },
      "/metrics": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true
      },
      "/healthz": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true
      }
    }
  }
});
