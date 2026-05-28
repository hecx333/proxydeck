import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Drawer, Form, Input, InputNumber, Select, Space, Switch, Table, Tooltip, Typography, message, Dropdown, MenuProps, Badge } from "antd";
import { 
  PlusOutlined,
  DownOutlined,
  EditOutlined,
  SyncOutlined,
  DeleteOutlined,
  CloudServerOutlined,
  ClockCircleOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  FolderOpenOutlined
} from "@ant-design/icons";
import { createSubscription, deleteSubscription, listSubscriptions, syncSubscription, updateSubscription } from "../api/subscriptions";
import { ConfirmButton } from "../components/ConfirmButton";
import { DateTimeText } from "../components/DateTimeText";
import { PageContainer } from "../components/PageContainer";
import { Subscription } from "../types";
import { useState } from "react";

export function SubscriptionsPage() {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Subscription | null>(null);
  
  const queryClient = useQueryClient();
  const { data = [], isLoading } = useQuery<Subscription[]>({ queryKey: ["subscriptions"], queryFn: listSubscriptions });
  
  const saveMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (editing) {
        await updateSubscription(editing.id, values);
        return { edited: true };
      }
      const created = await createSubscription(values);
      const syncResult = await syncSubscription(created.id);
      return { created, syncResult };
    },
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: ["subscriptions"] });
      await queryClient.invalidateQueries({ queryKey: ["nodes"] });
      setOpen(false);
      setEditing(null);
      if (result?.edited) {
        message.success("Subscription updated");
        return;
      }
      if (!result?.syncResult) {
        return;
      }
      const { imported_count } = result.syncResult;
      message.success(`Successfully imported ${imported_count} nodes. Health checks are running in the background.`);
    }
  });

  const syncMutation = useMutation({
    mutationFn: syncSubscription,
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: ["subscriptions"] });
      await queryClient.invalidateQueries({ queryKey: ["nodes"] });
      message.success(`Sync completed. Imported ${result.imported_count} nodes. Health checks are running in the background.`);
    }
  });

  // Format seconds to a readable interval string
  const formatInterval = (seconds: number) => {
    if (seconds < 60) return `${seconds}s`;
    const mins = Math.round(seconds / 60);
    if (mins < 60) return `${mins}m`;
    const hrs = Math.round(mins / 60);
    if (hrs < 24) return `${hrs}h`;
    const days = Math.round(hrs / 24);
    return `${days}d`;
  };

  return (
    <PageContainer 
      title="Subscriptions" 
      extra={
        <Button 
          type="primary" 
          icon={<PlusOutlined />} 
          onClick={() => { setEditing(null); setOpen(true); }}
          style={{ height: 38, borderRadius: 8, display: "flex", alignItems: "center" }}
        >
          Add Subscription
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
            title: "Name", 
            key: "sub_name",
            sorter: (left, right) => left.name.localeCompare(right.name),
            render: (_, row) => (
              <Space size={8}>
                <div style={{
                  width: 32,
                  height: 32,
                  borderRadius: 8,
                  background: "linear-gradient(135deg, rgba(13, 148, 136, 0.1), rgba(99, 102, 241, 0.05))",
                  display: "grid",
                  placeItems: "center",
                  color: "#0d9488"
                }}>
                  <FolderOpenOutlined style={{ fontSize: 15 }} />
                </div>
                <Space direction="vertical" size={0}>
                  <Typography.Text strong style={{ color: "#0f172a", fontSize: 14 }}>
                    {row.name}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 11, fontFamily: "monospace", display: "inline-block", maxWidth: 200, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {row.url}
                  </Typography.Text>
                </Space>
              </Space>
            )
          },
          { 
            title: "Type", 
            dataIndex: "type", 
            width: 100,
            sorter: (left, right) => left.type.localeCompare(right.type),
            render: (val) => <Badge status="processing" text={<span style={{ textTransform: "uppercase", fontSize: 11, fontWeight: 600, color: "#475569" }}>{val}</span>} />
          },
          { 
            title: "Status", 
            dataIndex: "enabled",
            width: 100,
            sorter: (left, right) => (left.enabled === right.enabled ? 0 : left.enabled ? -1 : 1),
            render: (enabled) => enabled ? (
              <Badge status="success" text={<span style={{ color: "#10b981", fontWeight: 500 }}>Active</span>} />
            ) : (
              <Badge status="default" text={<span style={{ color: "#94a3b8" }}>Inactive</span>} />
            )
          },
          { 
            title: "Sync Interval", 
            dataIndex: "sync_interval_seconds",
            width: 130,
            sorter: (left, right) => left.sync_interval_seconds - right.sync_interval_seconds,
            render: (val) => (
              <Space size={4} style={{ color: "#475569", fontSize: 13 }}>
                <ClockCircleOutlined style={{ color: "#94a3b8", fontSize: 12 }} />
                <span>Every {formatInterval(val)}</span>
              </Space>
            )
          },
          { 
            title: "Imported Nodes", 
            dataIndex: "node_count",
            width: 140,
            sorter: (left, right) => (left.node_count ?? 0) - (right.node_count ?? 0),
            render: (val) => (
              <Space size={6}>
                <CloudServerOutlined style={{ color: "#0d9488", fontSize: 13 }} />
                <span style={{ fontWeight: 600, color: "#334155", fontFamily: "monospace" }}>{val ?? 0}</span>
                <span style={{ color: "#94a3b8", fontSize: 12 }}>nodes</span>
              </Space>
            )
          },
          {
            title: "Sync Status",
            key: "sync_status",
            width: 130,
            sorter: (left, right) => (left.last_sync_status ?? "").localeCompare(right.last_sync_status ?? ""),
            render: (_, row) => {
              if (!row.last_sync_status) {
                return <span style={{ color: "#94a3b8", fontSize: 13 }}>Never synced</span>;
              }
              const isOk = row.last_sync_status === "ok";
              return (
                <Tooltip title={row.last_sync_detail || "No details"}>
                  <Space size={4} style={{ cursor: "help" }}>
                    {isOk ? (
                      <Badge status="success" text={<span style={{ color: "#10b981", fontWeight: 500 }}>Success</span>} />
                    ) : (
                      <Badge status="error" text={<span style={{ color: "#f43f5e", fontWeight: 500 }}>Failed</span>} />
                    )}
                  </Space>
                </Tooltip>
              );
            }
          },
          { 
            title: "Last Sync", 
            dataIndex: "last_sync_at",
            sorter: (left, right) => new Date(left.last_sync_at ?? 0).getTime() - new Date(right.last_sync_at ?? 0).getTime(), 
            render: (val) => <DateTimeText value={val} /> 
          },
          {
            title: "Actions",
            key: "actions",
            width: 100,
            render: (_, row) => {
              const menuItems: MenuProps["items"] = [
                {
                  key: "sync",
                  label: "Sync now",
                  icon: <SyncOutlined style={{ color: "#0d9488" }} />,
                  onClick: () => syncMutation.mutate(row.id)
                },
                {
                  key: "edit",
                  label: "Edit settings",
                  icon: <EditOutlined />,
                  onClick: () => { setEditing(row); setOpen(true); }
                },
                {
                  type: "divider"
                },
                {
                  key: "delete",
                  danger: true,
                  label: (
                    <ConfirmButton 
                      title="Delete subscription?" 
                      danger 
                      type="text"
                      style={{ padding: 0, height: "auto", color: "#f43f5e" }}
                      onConfirm={() => deleteSubscription(row.id).then(() => queryClient.invalidateQueries({ queryKey: ["subscriptions"] }))}
                    >
                      Delete sub
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
            <FolderOpenOutlined style={{ color: "#0d9488" }} />
            <span>{editing ? "Edit Subscription" : "Create Subscription"}</span>
          </Space>
        }
        onClose={() => { setOpen(false); setEditing(null); }} 
        width={440}
        style={{ backdropFilter: "blur(8px)" }}
      >
        <Form
          key={editing?.id ?? "new"}
          layout="vertical"
          initialValues={editing ?? { type: "singbox", enabled: true, sync_interval_seconds: 3600 }}
          onFinish={(values) => saveMutation.mutate(values)}
          style={{ marginTop: 8 }}
        >
          <Form.Item name="name" label="Subscription Name" rules={[{ required: true, message: "Please input subscription name" }]}>
            <Input placeholder="e.g. Premium Nodes US" size="large" />
          </Form.Item>
          <Form.Item name="type" label="Core/Format Type">
            <Select
              size="large"
              options={[
                { label: "Sing-box JSON format", value: "singbox" },
                { label: "Shadowrocket Base64 URI format", value: "shadowrocket" },
                { label: "Clash YAML format", value: "clash" },
                { label: "Mihomo YAML format", value: "mihomo" },
                { label: "Surge INI format", value: "surge" },
                { label: "Surfboard INI format", value: "surfboard" },
                { label: "Quantumult X line format", value: "quantumultx" },
                { label: "Manual proxy list URL", value: "manual" }
              ]}
            />
          </Form.Item>
          <Form.Item name="url" label="Subscription Provider URL" rules={[{ required: true, message: "Please input url" }]}>
            <Input placeholder="https://provider.com/sub/xxxx" size="large" />
          </Form.Item>
          
          <Form.Item name="enabled" label="Automatic Synced Status" valuePropName="checked">
            <Switch checkedChildren="ON" unCheckedChildren="OFF" />
          </Form.Item>
          
          <Form.Item name="sync_interval_seconds" label="Sync Interval (Seconds)">
            <InputNumber style={{ width: "100%" }} size="large" min={60} placeholder="3600" />
          </Form.Item>
          
          <Form.Item name="remark" label="Remarks">
            <Input.TextArea rows={3} placeholder="Notes about this subscription line..." />
          </Form.Item>
          
          <div style={{ marginTop: 24 }}>
            <Button block type="primary" htmlType="submit" size="large" loading={saveMutation.isPending} style={{ borderRadius: 10 }}>
              Save & Import Subscription
            </Button>
          </div>
        </Form>
      </Drawer>
    </PageContainer>
  );
}
