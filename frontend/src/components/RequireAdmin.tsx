import { useQuery } from "@tanstack/react-query";
import { Spin } from "antd";
import { Navigate, Outlet, useLocation } from "react-router-dom";
import { me } from "../api/auth";
import { useEffect } from "react";
import { useAuthStore } from "../store/auth";

export function RequireAdmin() {
  const location = useLocation();
  const setUsername = useAuthStore((state) => state.setUsername);
  const { data, isLoading, isError } = useQuery({
    queryKey: ["admin-me"],
    queryFn: me,
    retry: false
  });
  useEffect(() => {
    if (data?.username) {
      setUsername(data.username);
      return;
    }
    if (isError) {
      setUsername(null);
    }
  }, [data?.username, isError, setUsername]);

  if (isLoading) {
    return (
      <div style={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
        <Spin size="large" />
      </div>
    );
  }
  if (isError || !data?.username) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <Outlet />;
}
