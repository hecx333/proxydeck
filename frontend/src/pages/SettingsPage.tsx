import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Descriptions, Spin } from "antd";
import { getSettings } from "../api/settings";
import { PageContainer } from "../components/PageContainer";

export function SettingsPage() {
  const { data, isLoading } = useQuery({ queryKey: ["settings"], queryFn: getSettings });

  return (
    <PageContainer title="Settings">
      <Alert
        type="info"
        showIcon
        message="Runtime settings"
        description="当前页面展示后端实际加载的配置快照，便于核对网关监听、Redis、SQLite、Sticky 和健康检查参数。"
        style={{ marginBottom: 16 }}
      />
      <Card className="glass-panel" bordered={false}>
        {isLoading ? (
          <div style={{ minHeight: 160, display: "grid", placeItems: "center" }}>
            <Spin />
          </div>
        ) : (
          <Descriptions column={1}>
            <Descriptions.Item label="Proxy Gateway">{data?.server?.proxy_listen}</Descriptions.Item>
            <Descriptions.Item label="Admin API">{data?.server?.admin_listen}</Descriptions.Item>
            <Descriptions.Item label="Proxy Dial Timeout">{data?.server?.proxy_dial_timeout_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Proxy Idle Timeout">{data?.server?.proxy_idle_timeout_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Proxy Response Header Timeout">{data?.server?.proxy_response_header_timeout_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Proxy CONNECT Timeout">{data?.server?.proxy_connect_timeout_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="SQLite Path">{data?.sqlite?.path}</Descriptions.Item>
            <Descriptions.Item label="Redis">{`${data?.redis?.addr} / db ${data?.redis?.db}`}</Descriptions.Item>
            <Descriptions.Item label="Sticky TTL">{data?.sticky?.ttl_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Health Check Interval">{data?.healthcheck?.interval_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Health Check Timeout">{data?.healthcheck?.timeout_seconds} seconds</Descriptions.Item>
            <Descriptions.Item label="Health Check Max Fail Count">{data?.healthcheck?.max_fail_count}</Descriptions.Item>
            <Descriptions.Item label="Retry Max Attempts">{data?.retry?.max_attempts}</Descriptions.Item>
            <Descriptions.Item label="Retry Base Backoff">{data?.retry?.base_backoff_ms} ms</Descriptions.Item>
          </Descriptions>
        )}
      </Card>
    </PageContainer>
  );
}
