import { Suspense, lazy } from "react";
import { Spin } from "antd";
import { createBrowserRouter, Navigate } from "react-router-dom";

const RequireAdmin = lazy(() => import("../components/RequireAdmin").then((module) => ({ default: module.RequireAdmin })));
const AdminLayout = lazy(() => import("../layouts/AdminLayout").then((module) => ({ default: module.AdminLayout })));
const AuditLogsPage = lazy(() => import("../pages/AuditLogsPage").then((module) => ({ default: module.AuditLogsPage })));
const DashboardPage = lazy(() => import("../pages/DashboardPage").then((module) => ({ default: module.DashboardPage })));
const LoginPage = lazy(() => import("../pages/LoginPage").then((module) => ({ default: module.LoginPage })));
const NodesPage = lazy(() => import("../pages/NodesPage").then((module) => ({ default: module.NodesPage })));
const SettingsPage = lazy(() => import("../pages/SettingsPage").then((module) => ({ default: module.SettingsPage })));
const SubscriptionsPage = lazy(() => import("../pages/SubscriptionsPage").then((module) => ({ default: module.SubscriptionsPage })));
const TrafficPage = lazy(() => import("../pages/TrafficPage").then((module) => ({ default: module.TrafficPage })));
const UsersPage = lazy(() => import("../pages/UsersPage").then((module) => ({ default: module.UsersPage })));

function RouteFallback() {
  return (
    <div style={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
      <Spin size="large" />
    </div>
  );
}

function withSuspense(element: JSX.Element) {
  return <Suspense fallback={<RouteFallback />}>{element}</Suspense>;
}

export const router = createBrowserRouter([
  { path: "/", element: <Navigate to="/admin/dashboard" replace /> },
  { path: "/login", element: withSuspense(<LoginPage />) },
  {
    element: withSuspense(<RequireAdmin />),
    children: [
      {
        path: "/admin",
        element: withSuspense(<AdminLayout />),
        children: [
          { path: "dashboard", element: withSuspense(<DashboardPage />) },
          { path: "users", element: withSuspense(<UsersPage />) },
          { path: "subscriptions", element: withSuspense(<SubscriptionsPage />) },
          { path: "nodes", element: withSuspense(<NodesPage />) },
          { path: "traffic", element: withSuspense(<TrafficPage />) },
          { path: "audit-logs", element: withSuspense(<AuditLogsPage />) },
          { path: "settings", element: withSuspense(<SettingsPage />) }
        ]
      }
    ]
  }
]);
