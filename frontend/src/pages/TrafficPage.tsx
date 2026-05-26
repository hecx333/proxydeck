import { useQuery } from "@tanstack/react-query";
import { Card, Col, Row, Table, Typography, Space } from "antd";
import { 
  RiseOutlined, 
  UserOutlined, 
  GlobalOutlined, 
  EnvironmentOutlined,
  DatabaseOutlined 
} from "@ant-design/icons";
import { getTrafficStats } from "../api/stats";
import { Chart } from "../components/Chart";
import { MetricCard } from "../components/MetricCard";
import { PageContainer } from "../components/PageContainer";
import { formatBytes } from "../utils/format";

type RegionRow = { region: string; value: number };
type UserRow = { uid: string; used_bytes: number; total_requests: number };
type NodeRow = { tag: string; exit_ip: string; bytes: number; total_requests: number; latency_ms: number };

export function TrafficPage() {
  const { data } = useQuery({ queryKey: ["traffic"], queryFn: getTrafficStats });
  const topUserBytes = data?.top_users?.[0]?.used_bytes ?? 0;
  const topNodeBytes = data?.top_nodes?.[0]?.bytes ?? 0;
  const topRegionBytes = data?.top_regions?.[0]?.value ?? 0;
  const recent24hTotal = (data?.recent_24h ?? []).reduce((sum: number, item: { bytes: number }) => sum + item.bytes, 0);

  return (
    <PageContainer title="Traffic">
      <div className="metric-grid animate-fade-in" style={{ marginBottom: 20 }}>
        <MetricCard 
          title="Recent 24h Traffic" 
          value={formatBytes(recent24hTotal)} 
          icon={<RiseOutlined style={{ color: "#0d9488" }} />}
        />
        <MetricCard 
          title="Top User Traffic" 
          value={formatBytes(topUserBytes)} 
          icon={<UserOutlined style={{ color: "#6366f1" }} />}
        />
        <MetricCard 
          title="Top Node Traffic" 
          value={formatBytes(topNodeBytes)} 
          icon={<GlobalOutlined style={{ color: "#0ea5e9" }} />}
        />
        <MetricCard 
          title="Top Region Traffic" 
          value={formatBytes(topRegionBytes)} 
          icon={<EnvironmentOutlined style={{ color: "#f59e0b" }} />}
        />
      </div>

      <Row gutter={[20, 20]} className="animate-fade-in" style={{ animationDelay: "0.1s" }}>
        <Col xs={24} lg={16}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <RiseOutlined style={{ color: "#0d9488" }} />
                <span>Recent 24h Bandwidth Usage</span>
              </Space>
            }
          >
            <Chart
              style={{ height: 320 }}
              option={{
                tooltip: { 
                  trigger: "axis",
                  backgroundColor: "rgba(15, 23, 42, 0.9)",
                  borderWidth: 0,
                  textStyle: { color: "#fff" },
                  formatter: (params: any) => {
                    const item = params[0];
                    return `<div style={{padding: '4px 8px'}}>
                      <div style="font-size: 11px; color: #94a3b8">${item.name}</div>
                      <div style="font-weight: bold; margin-top: 4px">${formatBytes(item.value)}</div>
                    </div>`;
                  }
                },
                grid: { left: 48, right: 16, top: 24, bottom: 36 },
                xAxis: { 
                  type: "category", 
                  data: data?.recent_24h?.map((item: { hour: string }) => item.hour) ?? [],
                  axisLine: { lineStyle: { color: "rgba(15, 23, 42, 0.08)" } },
                  axisLabel: { color: "#64748b", fontWeight: 500 }
                },
                yAxis: { 
                  type: "value",
                  splitLine: { lineStyle: { color: "rgba(15, 23, 42, 0.04)" } },
                  axisLabel: { 
                    color: "#64748b", 
                    fontWeight: 500,
                    formatter: (val: number) => formatBytes(val)
                  }
                },
                series: [{
                  name: "Traffic",
                  type: "line",
                  smooth: true,
                  showSymbol: false,
                  lineStyle: { width: 3, color: "#0d9488" },
                  areaStyle: {
                    color: {
                      type: "linear",
                      x: 0,
                      y: 0,
                      x2: 0,
                      y2: 1,
                      colorStops: [
                        { offset: 0, color: "rgba(13, 148, 136, 0.35)" },
                        { offset: 1, color: "rgba(99, 102, 241, 0.01)" }
                      ]
                    }
                  },
                  data: data?.recent_24h?.map((item: { bytes: number }) => item.bytes) ?? []
                }]
              }}
            />
          </Card>
        </Col>
        
        <Col xs={24} lg={8}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <EnvironmentOutlined style={{ color: "#f59e0b" }} />
                <span>Region Traffic Ranking</span>
              </Space>
            }
          >
            <Table<RegionRow>
              pagination={false}
              rowKey="region"
              size="middle"
              dataSource={data?.top_regions ?? []}
              columns={[
                { 
                  title: "Region", 
                  dataIndex: "region", 
                  sorter: (left, right) => left.region.localeCompare(right.region),
                  render: (val) => <span style={{ fontWeight: 600, color: "#334155" }}>{val}</span>
                },
                { 
                  title: "Volume", 
                  sorter: (left, right) => left.value - right.value, 
                  defaultSortOrder: "descend", 
                  render: (_, row) => (
                    <span style={{ fontFamily: "monospace", fontWeight: 600, color: "#0d9488" }}>
                      {formatBytes(row.value)}
                    </span>
                  )
                }
              ]}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={[20, 20]} className="animate-fade-in" style={{ marginTop: 20, animationDelay: "0.2s" }}>
        <Col xs={24} lg={12}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <UserOutlined style={{ color: "#6366f1" }} />
                <span>Top Users by Bandwidth</span>
              </Space>
            }
          >
            <Table<UserRow>
              pagination={false}
              rowKey="uid"
              size="middle"
              dataSource={data?.top_users ?? []}
              columns={[
                { 
                  title: "UID", 
                  dataIndex: "uid", 
                  sorter: (left, right) => left.uid.localeCompare(right.uid),
                  render: (val) => <span style={{ fontWeight: 600, color: "#334155" }}>{val}</span>
                },
                { 
                  title: "Used Volume", 
                  sorter: (left, right) => left.used_bytes - right.used_bytes, 
                  defaultSortOrder: "descend", 
                  render: (_, row) => (
                    <span style={{ fontFamily: "monospace", fontWeight: 600, color: "#0d9488" }}>
                      {formatBytes(row.used_bytes)}
                    </span>
                  )
                },
                { 
                  title: "Requests", 
                  dataIndex: "total_requests", 
                  sorter: (left, right) => left.total_requests - right.total_requests,
                  render: (val) => <span style={{ fontFamily: "monospace", color: "#475569" }}>{val.toLocaleString()}</span>
                }
              ]}
            />
          </Card>
        </Col>
        
        <Col xs={24} lg={12}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <GlobalOutlined style={{ color: "#0ea5e9" }} />
                <span>Top Nodes by Bandwidth</span>
              </Space>
            }
          >
            <Table<NodeRow>
              pagination={false}
              rowKey="tag"
              size="middle"
              dataSource={data?.top_nodes ?? []}
              columns={[
                { 
                  title: "Tag", 
                  dataIndex: "tag", 
                  sorter: (left, right) => left.tag.localeCompare(right.tag),
                  render: (val) => <span style={{ fontWeight: 600, color: "#334155" }}>{val}</span>
                },
                { 
                  title: "Exit IP", 
                  dataIndex: "exit_ip", 
                  sorter: (left, right) => left.exit_ip.localeCompare(right.exit_ip),
                  render: (val) => <span style={{ fontFamily: "monospace", fontSize: 12 }}>{val}</span>
                },
                { 
                  title: "Volume", 
                  sorter: (left, right) => left.bytes - right.bytes, 
                  defaultSortOrder: "descend", 
                  render: (_, row) => (
                    <span style={{ fontFamily: "monospace", fontWeight: 600, color: "#0d9488" }}>
                      {formatBytes(row.bytes)}
                    </span>
                  )
                },
                { 
                  title: "Requests", 
                  dataIndex: "total_requests", 
                  sorter: (left, right) => left.total_requests - right.total_requests,
                  render: (val) => <span style={{ fontFamily: "monospace", color: "#475569" }}>{val.toLocaleString()}</span>
                },
                { 
                  title: "Latency", 
                  dataIndex: "latency_ms", 
                  sorter: (left, right) => left.latency_ms - right.latency_ms,
                  render: (val) => <span style={{ color: val < 150 ? "#10b981" : "#f59e0b", fontWeight: 500 }}>{val}ms</span>
                }
              ]}
            />
          </Card>
        </Col>
      </Row>
    </PageContainer>
  );
}
