import { Card, Space, Typography } from "antd";
import { ReactNode } from "react";

export function MetricCard({ 
  title, 
  value, 
  suffix, 
  icon,
  trend
}: { 
  title: string; 
  value: string | number; 
  suffix?: string;
  icon?: ReactNode;
  trend?: { type: "up" | "down"; text: string }
}) {
  return (
    <Card className="glass-panel metric-card-interactive" bordered={false} style={{ padding: "4px 8px" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
        <Space direction="vertical" size={4} style={{ width: "100%" }}>
          <Typography.Text style={{ color: "#64748b", fontSize: 13, fontWeight: 500, display: "block" }}>
            {title}
          </Typography.Text>
          <div style={{ display: "flex", alignItems: "baseline", gap: 4 }}>
            <Typography.Text style={{ fontSize: 24, fontWeight: 700, color: "#0f172a", fontFamily: '"Outfit", sans-serif' }}>
              {value}
            </Typography.Text>
            {suffix && (
              <Typography.Text style={{ fontSize: 13, color: "#64748b", fontWeight: 500 }}>
                {suffix}
              </Typography.Text>
            )}
          </div>
          {trend && (
            <div style={{ display: "flex", alignItems: "center", gap: 4, marginTop: 4 }}>
              <span style={{
                fontSize: 11,
                fontWeight: 600,
                color: trend.type === "up" ? "#10b981" : "#f43f5e",
                background: trend.type === "up" ? "rgba(16, 185, 129, 0.1)" : "rgba(244, 63, 94, 0.1)",
                padding: "2px 6px",
                borderRadius: 6
              }}>
                {trend.type === "up" ? "↑" : "↓"} {trend.text}
              </span>
            </div>
          )}
        </Space>
        {icon && (
          <div style={{
            width: 40,
            height: 40,
            borderRadius: 10,
            background: "linear-gradient(135deg, rgba(13, 148, 136, 0.1), rgba(99, 102, 241, 0.05))",
            display: "grid",
            placeItems: "center",
            color: "#0d9488",
            fontSize: 18,
            boxShadow: "inset 0 1px 1px rgba(255,255,255,0.6)"
          }}>
            {icon}
          </div>
        )}
      </div>
    </Card>
  );
}
