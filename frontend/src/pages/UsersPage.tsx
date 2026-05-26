import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, DatePicker, Drawer, Form, Input, InputNumber, Modal, Space, Table, message, Dropdown, MenuProps, Badge, Tooltip, Row, Col } from "antd";
import { 
  PlusOutlined, 
  DownOutlined, 
  EditOutlined, 
  StopOutlined, 
  CheckOutlined, 
  LockOutlined, 
  DeleteOutlined, 
  UserOutlined,
  DashboardOutlined 
} from "@ant-design/icons";
import dayjs from "dayjs";
import { createUser, deleteUser, listUsers, resetUserPassword, updateUser } from "../api/users";
import { BytesText } from "../components/BytesText";
import { ConfirmButton } from "../components/ConfirmButton";
import { DateTimeText } from "../components/DateTimeText";
import { PageContainer } from "../components/PageContainer";
import { QuotaInput } from "../components/QuotaInput";
import { formatBytes } from "../utils/format";
import { User } from "../types";
import { useState } from "react";

export function UsersPage() {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [passwordModalOpen, setPasswordModalOpen] = useState(false);
  const [passwordTarget, setPasswordTarget] = useState<User | null>(null);
  
  const queryClient = useQueryClient();
  const { data = [], isLoading } = useQuery<User[]>({ queryKey: ["users"], queryFn: listUsers });
  
  const saveMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (editing) return updateUser(editing.id, values);
      return createUser(values);
    },
    onSuccess: () => {
      message.success("User saved successfully");
      setOpen(false);
      setEditing(null);
      queryClient.invalidateQueries({ queryKey: ["users"] });
    }
  });

  const deleteMutation = useMutation({
    mutationFn: deleteUser,
    onSuccess: () => {
      message.success("User deleted");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    }
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => updateUser(id, { enabled }),
    onSuccess: () => {
      message.success("User status updated");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    }
  });

  const passwordMutation = useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) => resetUserPassword(id, password),
    onSuccess: () => {
      message.success("Password reset completed");
      setPasswordModalOpen(false);
      setPasswordTarget(null);
    }
  });

  // Helper to render quota progress bar
  const renderQuotaProgress = (used: number, quota: number) => {
    if (!quota) return <span style={{ color: "#64748b", fontSize: 13 }}>—</span>;
    const pct = Math.min(100, Math.round((used / quota) * 100));
    
    let fillClass = "";
    if (pct >= 90) fillClass = " danger";
    else if (pct >= 75) fillClass = " warning";

    return (
      <div className="quota-progress-container" style={{ minWidth: 150 }}>
        <div style={{ display: "flex", justifyContent: "space-between", fontSize: 11, fontWeight: 500 }}>
          <span style={{ color: "#334155" }}>{formatBytes(used)}</span>
          <span style={{ color: "#64748b" }}>/ {formatBytes(quota)} ({pct}%)</span>
        </div>
        <div className="quota-progress-bar">
          <div className={`quota-progress-fill${fillClass}`} style={{ width: `${pct}%` }} />
        </div>
      </div>
    );
  };

  return (
    <PageContainer
      title="Users"
      extra={
        <Button 
          type="primary" 
          icon={<PlusOutlined />} 
          onClick={() => { setEditing(null); setOpen(true); }}
          style={{ height: 38, borderRadius: 8, display: "flex", alignItems: "center" }}
        >
          Create User
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        className="glass-panel animate-fade-in"
        pagination={{ pageSize: 10 }}
        scroll={{ x: "max-content" }}
        columns={[
          { 
            title: "User Info", 
            key: "user_info",
            sorter: (left, right) => left.uid.localeCompare(right.uid),
            render: (_, row) => (
              <Space size={8}>
                <div style={{
                  width: 32,
                  height: 32,
                  borderRadius: "50%",
                  background: "linear-gradient(135deg, rgba(13, 148, 136, 0.1), rgba(99, 102, 241, 0.05))",
                  display: "grid",
                  placeItems: "center",
                  color: "#0d9488"
                }}>
                  <UserOutlined style={{ fontSize: 14 }} />
                </div>
                <span style={{ fontWeight: 600, color: "#0f172a", fontSize: 14 }}>{row.uid}</span>
              </Space>
            )
          },
          { 
            title: "Status", 
            dataIndex: "enabled", 
            width: 100,
            sorter: (left, right) => (left.enabled === right.enabled ? 0 : left.enabled ? -1 : 1),
            render: (enabled) => enabled ? (
              <Badge status="success" text={<span style={{ color: "#10b981", fontWeight: 500 }}>Active</span>} />
            ) : (
              <Badge status="default" text={<span style={{ color: "#94a3b8" }}>Disabled</span>} />
            )
          },
          { 
            title: "Traffic Quota Usage", 
            key: "quota_usage",
            sorter: (left, right) => left.used_bytes - right.used_bytes, 
            render: (_, row) => renderQuotaProgress(row.used_bytes, row.quota_bytes)
          },
          { 
            title: "Total Requests", 
            dataIndex: "total_requests", 
            width: 130,
            sorter: (left, right) => left.total_requests - right.total_requests,
            render: (val) => (
              <span style={{ fontFamily: "monospace", fontSize: 13, color: "#475569", fontWeight: 500 }}>
                {val.toLocaleString()}
              </span>
            )
          },
          { 
            title: "Concurrency (Active / Limit)", 
            key: "concurrency",
            sorter: (left, right) => (left.current_concurrency ?? 0) - (right.current_concurrency ?? 0), 
            render: (_, row) => {
              const active = row.current_concurrency ?? 0;
              const hasActive = active > 0;
              return (
                <Tooltip title="Current active connections vs maximum allowed">
                  <Space size={6}>
                    <Badge status={hasActive ? "processing" : "default"} />
                    <span style={{ 
                      fontWeight: 600, 
                      color: hasActive ? "#0d9488" : "#475569",
                      fontFamily: "monospace" 
                    }}>
                      {active}
                    </span>
                    <span style={{ color: "#94a3b8", fontSize: 12 }}>/</span>
                    <span style={{ color: "#64748b", fontFamily: "monospace" }}>{row.max_concurrency}</span>
                  </Space>
                </Tooltip>
              );
            }
          },
          { 
            title: "Expired At", 
            dataIndex: "expired_at",
            sorter: (left, right) => new Date(left.expired_at ?? 0).getTime() - new Date(right.expired_at ?? 0).getTime(), 
            render: (val) => <DateTimeText value={val} /> 
          },
          { 
            title: "Created At", 
            dataIndex: "created_at", 
            defaultSortOrder: "descend", 
            sorter: (left, right) => new Date(left.created_at).getTime() - new Date(right.created_at).getTime(), 
            render: (val) => <DateTimeText value={val} /> 
          },
          {
            title: "Actions",
            key: "actions",
            width: 100,
            render: (_, row) => {
              const menuItems: MenuProps["items"] = [
                {
                  key: "edit",
                  label: "Edit settings",
                  icon: <EditOutlined />,
                  onClick: () => { setEditing(row); setOpen(true); }
                },
                {
                  key: "toggle",
                  label: row.enabled ? "Disable user" : "Enable user",
                  icon: row.enabled ? <StopOutlined style={{ color: "#eab308" }} /> : <CheckOutlined style={{ color: "#10b981" }} />,
                  onClick: () => toggleMutation.mutate({ id: row.id, enabled: !row.enabled })
                },
                {
                  key: "password",
                  label: "Reset password",
                  icon: <LockOutlined style={{ color: "#0d9488" }} />,
                  onClick: () => { setPasswordTarget(row); setPasswordModalOpen(true); }
                },
                {
                  type: "divider"
                },
                {
                  key: "delete",
                  danger: true,
                  label: (
                    <ConfirmButton 
                      title="Delete this user?" 
                      danger 
                      type="text"
                      style={{ padding: 0, height: "auto", color: "#f43f5e" }}
                      onConfirm={() => deleteMutation.mutate(row.id)}
                    >
                      Delete user
                    </ConfirmButton>
                  ),
                  icon: <DeleteOutlined style={{ color: "#f43f5e" }} />
                }
              ];

              return (
                <Dropdown menu={{ items: menuItems }} trigger={["click"]}>
                  <Button size="small" style={{ borderRadius: 6 }}>
                    Actions <DownOutlined style={{ fontSize: 10 }} />
                  </Button>
                </Dropdown>
              );
            }
          }
        ]}
      />
      
      <Drawer 
        open={open} 
        title={
          <Space>
            <DashboardOutlined style={{ color: "#0d9488" }} />
            <span>{editing ? `Edit User: ${editing.uid}` : "Create User"}</span>
          </Space>
        }
        onClose={() => { setOpen(false); setEditing(null); }} 
        width={420}
        style={{ backdropFilter: "blur(8px)" }}
      >
        <Form
          key={editing?.id ?? "new"}
          layout="vertical"
          initialValues={editing ? { ...editing, expired_at: editing.expired_at ? dayjs(editing.expired_at) : null } : { enabled: true, max_concurrency: 100 }}
          onFinish={(values) => saveMutation.mutate({ ...values, expired_at: values.expired_at ? values.expired_at.toISOString() : null })}
        >
          <Form.Item name="uid" label="UID (Username)" rules={[{ required: true, message: "Please input UID" }]}>
            <Input disabled={Boolean(editing)} size="large" prefix={<UserOutlined style={{ color: "#94a3b8" }} />} />
          </Form.Item>
          <Form.Item name="password" label="Password" rules={editing ? [] : [{ required: true, message: "Please enter password" }]}>
            <Input.Password size="large" prefix={<LockOutlined style={{ color: "#94a3b8" }} />} />
          </Form.Item>
          <Form.Item name="enabled" label="Account Enabled" valuePropName="checked">
            <Form.Item name="enabled" valuePropName="checked" noStyle>
              <Button 
                onClick={() => {}}
                type="dashed"
                style={{ width: "100%", height: 40, textAlign: "left", display: "flex", alignItems: "center", justifyContent: "space-between" }}
              >
                <span>Active Status</span>
                <span style={{ color: "#0d9488" }}>Configure below</span>
              </Button>
            </Form.Item>
          </Form.Item>
          <Form.Item name="quota_bytes" label="Traffic Quota Limit" rules={[{ required: true, message: "Please input quota" }]}>
            <QuotaInput />
          </Form.Item>

          <Form.Item name="max_concurrency" label="Max Concurrency Limit" rules={[{ required: true, message: "Please input max concurrency" }]}>
            <InputNumber style={{ width: "100%" }} min={1} size="large" />
          </Form.Item>
          
          <Form.Item name="expired_at" label="Expiration Date">
            <DatePicker showTime style={{ width: "100%" }} format="YYYY-MM-DD HH:mm:ss" />
          </Form.Item>
          <Form.Item name="remark" label="Administrative Notes">
            <Input.TextArea rows={3} placeholder="Optional notes about this user..." />
          </Form.Item>
          <div style={{ marginTop: 24 }}>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending} block size="large" style={{ borderRadius: 10 }}>
              Save User Config
            </Button>
          </div>
        </Form>
      </Drawer>

      <Modal
        open={passwordModalOpen}
        title={
          <Space>
            <LockOutlined style={{ color: "#f43f5e" }} />
            <span>Reset Password: {passwordTarget?.uid}</span>
          </Space>
        }
        onCancel={() => { setPasswordModalOpen(false); setPasswordTarget(null); }}
        footer={null}
        width={380}
      >
        <Form
          layout="vertical"
          onFinish={(values) => {
            if (!passwordTarget) return;
            passwordMutation.mutate({ id: passwordTarget.id, password: values.password });
          }}
          style={{ marginTop: 16 }}
        >
          <Form.Item name="password" label="New Password" rules={[{ required: true, message: "Password is required" }, { min: 6, message: "Minimum 6 characters" }]}>
            <Input.Password size="large" prefix={<LockOutlined style={{ color: "#94a3b8" }} />} />
          </Form.Item>
          <Button htmlType="submit" type="primary" loading={passwordMutation.isPending} block size="large" style={{ borderRadius: 8, marginTop: 12 }}>
            Confirm Reset
          </Button>
        </Form>
      </Modal>
    </PageContainer>
  );
}
