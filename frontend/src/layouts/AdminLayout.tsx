import { LogoutOutlined, RadarChartOutlined, TeamOutlined, CloudServerOutlined, ApartmentOutlined, DatabaseOutlined, SettingOutlined } from "@ant-design/icons";
import { Layout, Menu, Space, Typography, Button } from "antd";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { logout } from "../api/auth";
import { BrandMark } from "../components/BrandMark";
import { useAuthStore } from "../store/auth";

const items = [
  { key: "/admin/dashboard", icon: <RadarChartOutlined style={{ fontSize: 16 }} />, label: "Dashboard" },
  { key: "/admin/users", icon: <TeamOutlined style={{ fontSize: 16 }} />, label: "Users" },
  { key: "/admin/subscriptions", icon: <ApartmentOutlined style={{ fontSize: 16 }} />, label: "Subscriptions" },
  { key: "/admin/nodes", icon: <CloudServerOutlined style={{ fontSize: 16 }} />, label: "Nodes" },
  { key: "/admin/traffic", icon: <DatabaseOutlined style={{ fontSize: 16 }} />, label: "Traffic" },
  { key: "/admin/audit-logs", icon: <DatabaseOutlined style={{ fontSize: 16 }} />, label: "Audit Logs" },
  { key: "/admin/settings", icon: <SettingOutlined style={{ fontSize: 16 }} />, label: "Settings" }
];

export function AdminLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const username = useAuthStore((state) => state.username);
  const setUsername = useAuthStore((state) => state.setUsername);

  return (
    <Layout style={{ minHeight: "100vh", background: "transparent" }}>
      <Layout.Sider 
        width={260} 
        className="glass-sider"
        style={{ height: "100vh", position: "sticky", top: 0, left: 0 }}
      >
        <div style={{ padding: "32px 24px 20px 24px" }}>
          <BrandMark size={34} showWordmark inverse />
          <Typography.Text style={{ color: "rgba(255,255,255,0.45)", fontSize: 10, display: "block", marginTop: 8, letterSpacing: "0.5px" }}>
            REGION-AWARE GATEWAY
          </Typography.Text>
        </div>
        <Menu
          theme="dark"
          selectedKeys={[location.pathname]}
          items={items}
          onClick={({ key }) => navigate(key)}
          style={{ background: "transparent", borderInlineEnd: 0, padding: "8px 12px" }}
        />
      </Layout.Sider>
      <Layout style={{ background: "transparent" }}>
        <Layout.Header style={{ background: "transparent", padding: "16px 32px 0 32px", height: "auto" }}>
          <div className="glass-panel" style={{ borderRadius: 12, padding: "10px 20px", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <Space size={8} align="center">
              <div style={{ width: 8, height: 8, borderRadius: "50%", background: "#10b981", boxShadow: "0 0 8px #10b981" }} />
              <Typography.Text strong style={{ color: "#334155", fontSize: 13 }}>Gateway Online</Typography.Text>
            </Space>
            <Space size={16} align="center">
              <Typography.Text style={{ color: "#64748b", fontSize: 13 }}>
                {username ? (
                  <span>Signed in as <strong style={{ color: "#0f172a" }}>{username}</strong></span>
                ) : (
                  "ProxyDeck Admin"
                )}
              </Typography.Text>
              <Button 
                type="text" 
                danger 
                icon={<LogoutOutlined />} 
                style={{ display: "flex", alignItems: "center", gap: 4, height: 32, borderRadius: 8 }}
                onClick={async () => {
                  await logout();
                  setUsername(null);
                  navigate("/login");
                }}
              >
                Logout
              </Button>
            </Space>
          </div>
        </Layout.Header>
        <Layout.Content style={{ overflowY: "auto" }}>
          <Outlet />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
