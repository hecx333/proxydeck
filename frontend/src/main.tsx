import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfigProvider, theme } from "antd";
import { RouterProvider } from "react-router-dom";
import { router } from "./router";
import "./styles.css";

const queryClient = new QueryClient();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          colorPrimary: "#0d9488",
          colorInfo: "#0ea5e9",
          colorSuccess: "#10b981",
          colorWarning: "#f59e0b",
          colorError: "#f43f5e",
          borderRadius: 12,
          fontFamily: '"Outfit", "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        },
        components: {
          Button: {
            controlHeightLG: 44,
            borderRadiusLG: 10,
          },
          Input: {
            controlHeightLG: 44,
            borderRadiusLG: 10,
          },
          Card: {
            borderRadiusLG: 16,
          },
          Table: {
            borderRadius: 16,
          }
        }
      }}
    >
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ConfigProvider>
  </React.StrictMode>
);
