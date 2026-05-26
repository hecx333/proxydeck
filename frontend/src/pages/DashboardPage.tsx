import { useQuery } from "@tanstack/react-query";
import { Card, Col, List, Row, Space, Typography, Tag } from "antd";
import { 
  TeamOutlined, 
  UserOutlined, 
  GlobalOutlined, 
  CheckCircleOutlined, 
  CloseCircleOutlined, 
  SwapOutlined, 
  DatabaseOutlined, 
  ArrowUpOutlined,
  AlertOutlined 
} from "@ant-design/icons";
import { getOverview, type OverviewErrorLog, type OverviewStats } from "../api/stats";
import { Chart } from "../components/Chart";
import { DateTimeText } from "../components/DateTimeText";
import { MetricCard } from "../components/MetricCard";
import { PageContainer } from "../components/PageContainer";
import { formatBytes } from "../utils/format";

export function DashboardPage() {
  const { data, isLoading } = useQuery<OverviewStats>({ queryKey: ["overview"], queryFn: getOverview });
  const regionData = Object.entries(data?.region_counts ?? {}).map(([name, value]) => ({ name, value }));

  return (
    <PageContainer title="Dashboard">
      <div className="metric-grid animate-fade-in">
        <MetricCard 
          title="Total Users" 
          value={data?.total_users ?? 0} 
          icon={<TeamOutlined />}
          trend={{ type: "up", text: "Registered accounts" }}
        />
        <MetricCard 
          title="Active Users" 
          value={data?.active_users ?? 0} 
          icon={<UserOutlined />}
          trend={{ type: "up", text: "Active sessions" }}
        />
        <MetricCard 
          title="Total Nodes" 
          value={data?.total_nodes ?? 0} 
          icon={<GlobalOutlined />}
        />
        <MetricCard 
          title="Healthy Nodes" 
          value={data?.healthy_nodes ?? 0} 
          icon={<CheckCircleOutlined style={{ color: "#10b981" }} />}
        />
        <MetricCard 
          title="Unhealthy Nodes" 
          value={data?.unhealthy_nodes ?? 0} 
          icon={<CloseCircleOutlined style={{ color: data?.unhealthy_nodes ? "#f43f5e" : "#94a3b8" }} />}
        />
        <MetricCard 
          title="Active Connections" 
          value={data?.active_connections ?? 0} 
          icon={<SwapOutlined />}
          trend={{ type: "up", text: "Live connections" }}
        />
        <MetricCard 
          title="Total Traffic" 
          value={formatBytes(data?.total_traffic ?? 0)} 
          icon={<DatabaseOutlined />}
        />
        <MetricCard 
          title="Today Traffic" 
          value={formatBytes(data?.today_traffic ?? 0)} 
          icon={<ArrowUpOutlined style={{ color: "#0ea5e9" }} />}
        />
        <MetricCard 
          title="Total Requests" 
          value={data?.total_requests ?? 0} 
          icon={<DatabaseOutlined />}
        />
      </div>
      
      <Row gutter={[20, 20]} className="animate-fade-in" style={{ animationDelay: "0.15s" }}>
        <Col xs={24} lg={13}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <GlobalOutlined style={{ color: "#0d9488" }} />
                <span>Region Counts Breakdown</span>
              </Space>
            }
          >
            {regionData.length === 0 ? (
              <div style={{ height: 320, display: "grid", placeItems: "center", color: "#64748b" }}>
                No active region distribution data available
              </div>
            ) : (
              <Chart
                style={{ height: 320 }}
                option={{
                  color: ['#0d9488', '#6366f1', '#0ea5e9', '#f59e0b', '#10b981', '#ec4899'],
                  tooltip: { 
                    trigger: "item",
                    backgroundColor: "rgba(15, 23, 42, 0.9)",
                    borderWidth: 0,
                    textStyle: { color: "#fff" }
                  },
                  legend: { 
                    bottom: 0,
                    icon: "circle",
                    itemWidth: 10,
                    itemHeight: 10,
                    textStyle: { color: "#475569", fontWeight: 500 }
                  },
                  series: [{ 
                    name: "Nodes by Region",
                    type: "pie", 
                    radius: ["45%", "72%"], 
                    avoidLabelOverlap: false,
                    itemStyle: {
                      borderRadius: 8,
                      borderColor: "#ffffff",
                      borderWidth: 2
                    },
                    label: {
                      show: false,
                      position: "center"
                    },
                    emphasis: {
                      label: {
                        show: true,
                        fontSize: 16,
                        fontWeight: "bold"
                      }
                    },
                    data: regionData 
                  }]
                }}
              />
            )}
          </Card>
        </Col>
        
        <Col xs={24} lg={11}>
          <Card 
            className="glass-panel" 
            bordered={false} 
            title={
              <Space>
                <AlertOutlined style={{ color: "#f43f5e" }} />
                <span>Recent Error Logs</span>
              </Space>
            }
          >
            <List
              locale={{ emptyText: "No recent error logs" }}
              dataSource={data?.recent_errors ?? []}
              style={{ maxHeight: 320, overflowY: "auto", paddingRight: 4 }}
              renderItem={(item: OverviewErrorLog) => (
                <List.Item style={{ 
                  borderBottom: "1px solid rgba(15, 23, 42, 0.04)", 
                  padding: "12px 14px",
                  marginBottom: 8,
                  borderRadius: 10,
                  background: "rgba(244, 63, 94, 0.03)",
                  borderLeft: "4px solid #f43f5e"
                }}>
                  <div style={{ width: "100%" }}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 4 }}>
                      <Typography.Text strong style={{ color: "#0f172a", fontSize: 13 }}>
                        {item.action}
                      </Typography.Text>
                      <Tag color="red" bordered={false} style={{ margin: 0, fontSize: 10 }}>Error</Tag>
                    </div>
                    <div style={{ display: "flex", flexDirection: "column", gap: 2, marginBottom: 4 }}>
                      <Typography.Text type="secondary" style={{ fontSize: 11, fontFamily: "monospace" }}>
                        Target: {item.target}
                      </Typography.Text>
                      <Typography.Text style={{ fontSize: 12, color: "#475569" }}>
                        {item.detail || "No detail"}
                      </Typography.Text>
                    </div>
                    <div style={{ textAlign: "right" }}>
                      <DateTimeText value={item.created_at} />
                    </div>
                  </div>
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </PageContainer>
  );
}
