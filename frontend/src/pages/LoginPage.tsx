import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Card, Form, Input, Typography, message, Space } from "antd";
import { UserOutlined, LockOutlined } from "@ant-design/icons";
import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { login, me } from "../api/auth";
import { BrandMark } from "../components/BrandMark";
import { useAuthStore } from "../store/auth";

export function LoginPage() {
  const navigate = useNavigate();
  const setUsername = useAuthStore((state) => state.setUsername);
  const { data } = useQuery({
    queryKey: ["admin-me-login"],
    queryFn: me,
    retry: false,
    staleTime: 0
  });

  useEffect(() => {
    if (data?.username) {
      setUsername(data.username);
      navigate("/admin/dashboard");
    }
  }, [data, navigate, setUsername]);

  const mutation = useMutation({
    mutationFn: login,
    onSuccess: (payload) => {
      setUsername(payload.username);
      navigate("/admin/dashboard");
      message.success("Logged in successfully");
    },
    onError: () => message.error("Login failed, please check your credentials")
  });

  return (
    <div style={{ 
      minHeight: "100vh", 
      display: "grid", 
      placeItems: "center", 
      padding: 24,
      background: `
        radial-gradient(circle at 10% 20%, rgba(13, 148, 136, 0.15) 0%, transparent 40%),
        radial-gradient(circle at 90% 80%, rgba(99, 102, 241, 0.12) 0%, transparent 40%),
        linear-gradient(135deg, #0f172a 0%, #1e293b 100%)
      `
    }}>
      <Card 
        className="glass-panel animate-fade-in" 
        bordered={false} 
        style={{ 
          width: "min(440px, 100%)", 
          borderRadius: 24,
          background: "rgba(30, 41, 59, 0.7)",
          borderColor: "rgba(255, 255, 255, 0.05)",
          boxShadow: "0 20px 40px rgba(0, 0, 0, 0.3)"
        }}
      >
        <div style={{ textAlign: "center", marginBottom: 28 }}>
          <Space direction="vertical" size={14}>
            <div style={{ display: "flex", justifyContent: "center" }}>
              <BrandMark size={56} showWordmark stacked inverse />
            </div>
            <Typography.Text style={{ color: "#94a3b8", fontSize: 13, display: "block" }}>
              Region-aware sticky proxy control panel
            </Typography.Text>
          </Space>
        </div>

        <Form 
          layout="vertical" 
          onFinish={(values) => mutation.mutate(values)}
          requiredMark={false}
        >
          <Form.Item 
            name="username" 
            label={<span style={{ color: "#94a3b8", fontWeight: 500, fontSize: 13 }}>Username</span>} 
            rules={[{ required: true, message: "Please input username" }]}
          >
            <Input 
              size="large" 
              prefix={<UserOutlined style={{ color: "#475569" }} />} 
              placeholder="Enter your admin account"
              style={{ 
                background: "rgba(15, 23, 42, 0.4)", 
                borderColor: "rgba(255, 255, 255, 0.1)", 
                color: "#ffffff",
                borderRadius: 10
              }}
            />
          </Form.Item>
          
          <Form.Item 
            name="password" 
            label={<span style={{ color: "#94a3b8", fontWeight: 500, fontSize: 13 }}>Password</span>} 
            rules={[{ required: true, message: "Please input password" }]}
          >
            <Input.Password 
              size="large" 
              prefix={<LockOutlined style={{ color: "#475569" }} />} 
              placeholder="Enter account password"
              style={{ 
                background: "rgba(15, 23, 42, 0.4)", 
                borderColor: "rgba(255, 255, 255, 0.1)", 
                color: "#ffffff",
                borderRadius: 10
              }}
            />
          </Form.Item>
          
          <Button 
            type="primary" 
            htmlType="submit" 
            loading={mutation.isPending} 
            size="large" 
            block
            style={{ 
              borderRadius: 10, 
              height: 46, 
              background: "linear-gradient(135deg, #0d9488, #0ea5e9)",
              border: 0,
              fontWeight: 600,
              boxShadow: "0 4px 12px rgba(13, 148, 136, 0.2)",
              marginTop: 12
            }}
          >
            Sign In Console
          </Button>
        </Form>
      </Card>
    </div>
  );
}
