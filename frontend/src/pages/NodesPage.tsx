import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Drawer, Input, Space, Table, Dropdown, MenuProps, Tooltip, Tag, Typography, Badge, Row, Col } from "antd";
import { 
  FilterOutlined, 
  SyncOutlined, 
  DownOutlined, 
  DeleteOutlined, 
  EyeOutlined, 
  CheckOutlined, 
  PlayCircleOutlined, 
  StopOutlined,
  SearchOutlined,
  EnvironmentOutlined
} from "@ant-design/icons";
import { useState } from "react";
import { checkNode, deleteNode, getNode, listNodes, toggleNodeDisabled } from "../api/nodes";
import { ConfirmButton } from "../components/ConfirmButton";
import { DateTimeText } from "../components/DateTimeText";
import { JsonViewer } from "../components/JsonViewer";
import { PageContainer } from "../components/PageContainer";
import { RegionTag } from "../components/RegionTag";
import { ProxyNode } from "../types";

export function NodesPage() {
  const [keyword, setKeyword] = useState("");
  const [region, setRegion] = useState("");
  const [city, setCity] = useState("");
  const [asn, setASN] = useState("");
  const [isp, setISP] = useState("");
  
  const [queryKeyword, setQueryKeyword] = useState("");
  const [queryRegion, setQueryRegion] = useState("");
  const [queryCity, setQueryCity] = useState("");
  const [queryASN, setQueryASN] = useState("");
  const [queryISP, setQueryISP] = useState("");
  
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  
  const queryClient = useQueryClient();
  const { data = [], isLoading } = useQuery<ProxyNode[]>({
    queryKey: ["nodes", queryKeyword, queryRegion, queryCity, queryASN, queryISP],
    queryFn: () => listNodes({ keyword: queryKeyword, region: queryRegion, city: queryCity, asn: queryASN, isp: queryISP })
  });
  
  const { data: detail } = useQuery({ queryKey: ["node", selectedID], queryFn: () => getNode(selectedID!), enabled: selectedID !== null });
  
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["nodes"] });
  
  const disableMutation = useMutation({
    mutationFn: ({ id, disabled }: { id: number; disabled: boolean }) => toggleNodeDisabled(id, disabled),
    onSuccess: refresh
  });
  
  const checkMutation = useMutation({
    mutationFn: checkNode,
    onSuccess: refresh
  });

  const applyFilters = () => {
    setQueryKeyword(keyword);
    setQueryRegion(region);
    setQueryCity(city);
    setQueryASN(asn);
    setQueryISP(isp);
  };

  const resetFilters = () => {
    setKeyword("");
    setRegion("");
    setCity("");
    setASN("");
    setISP("");
    setQueryKeyword("");
    setQueryRegion("");
    setQueryCity("");
    setQueryASN("");
    setQueryISP("");
  };

  // Visual latency thresholds
  const getLatencyTag = (ms: number) => {
    if (ms <= 0) return <Tag color="default">N/A</Tag>;
    if (ms < 150) return <Tag color="green" bordered={false}>{ms} ms</Tag>;
    if (ms < 400) return <Tag color="warning" bordered={false}>{ms} ms</Tag>;
    return <Tag color="error" bordered={false}>{ms} ms</Tag>;
  };

  return (
    <PageContainer
      title="Nodes"
      extra={
        <Space>
          <Button 
            icon={<FilterOutlined />} 
            onClick={() => setFiltersOpen(!filtersOpen)}
            type={filtersOpen ? "primary" : "default"}
          >
            Filters {filtersOpen ? "Open" : ""}
          </Button>
          <Button icon={<SyncOutlined />} onClick={refresh}>
            Refresh
          </Button>
        </Space>
      }
    >
      {/* Premium Collapsible Filters */}
      <div className={`filter-card glass-panel ${filtersOpen ? "expanded" : ""}`} style={{ 
        maxHeight: filtersOpen ? 300 : 0, 
        overflow: "hidden", 
        padding: filtersOpen ? "20px" : "0px",
        marginBottom: filtersOpen ? "20px" : "0px",
        opacity: filtersOpen ? 1 : 0,
        transition: "all 0.3s cubic-bezier(0.4, 0, 0.2, 1)"
      }}>
        {filtersOpen && (
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Row gutter={[16, 16]}>
              <Col xs={24} sm={12} md={6}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>Search Host / Tag / IP</Typography.Text>
                <Input 
                  placeholder="e.g. us-node-01" 
                  value={keyword} 
                  onChange={(e) => setKeyword(e.target.value)} 
                  prefix={<SearchOutlined style={{ color: "#94a3b8" }} />}
                  style={{ marginTop: 6 }}
                />
              </Col>
              <Col xs={24} sm={12} md={4}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>Region</Typography.Text>
                <Input 
                  placeholder="e.g. US" 
                  value={region} 
                  onChange={(e) => setRegion(e.target.value)} 
                  prefix={<EnvironmentOutlined style={{ color: "#94a3b8" }} />}
                  style={{ marginTop: 6 }}
                />
              </Col>
              <Col xs={24} sm={12} md={4}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>City</Typography.Text>
                <Input 
                  placeholder="e.g. San Jose" 
                  value={city} 
                  onChange={(e) => setCity(e.target.value)} 
                  style={{ marginTop: 6 }}
                />
              </Col>
              <Col xs={24} sm={12} md={5}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>ASN</Typography.Text>
                <Input 
                  placeholder="e.g. AS16509" 
                  value={asn} 
                  onChange={(e) => setASN(e.target.value)} 
                  style={{ marginTop: 6 }}
                />
              </Col>
              <Col xs={24} sm={12} md={5}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>ISP</Typography.Text>
                <Input 
                  placeholder="e.g. Amazon" 
                  value={isp} 
                  onChange={(e) => setISP(e.target.value)} 
                  style={{ marginTop: 6 }}
                />
              </Col>
            </Row>
            <div style={{ display: "flex", justifyContent: "flex-end", gap: 10 }}>
              <Button onClick={resetFilters}>Reset</Button>
              <Button type="primary" onClick={applyFilters}>Apply Filters</Button>
            </div>
          </Space>
        )}
      </div>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        className="glass-panel animate-fade-in"
        pagination={{ pageSize: 10, showSizeChanger: true }}
        scroll={{ x: "max-content" }}
        columns={[
          { 
            title: "Node Info", 
            key: "node_info",
            sorter: (left, right) => left.tag.localeCompare(right.tag),
            render: (_, row) => (
              <Space direction="vertical" size={2}>
                <Typography.Text strong style={{ color: "#0f172a" }}>{row.tag}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 11, fontFamily: "monospace" }}>
                  {row.host}:{row.port}
                </Typography.Text>
              </Space>
            )
          },
          { 
            title: "Protocol", 
            dataIndex: "protocol", 
            width: 90,
            sorter: (left, right) => left.protocol.localeCompare(right.protocol),
            render: (val) => <Tag color="blue" bordered={false} style={{ textTransform: "uppercase", fontSize: 11 }}>{val}</Tag>
          },
          { 
            title: "Region (Expected / Detected)", 
            key: "region_matching",
            sorter: (left, right) => (left.expected_region ?? "").localeCompare(right.expected_region ?? ""),
            render: (_, row) => {
              const matches = row.expected_region === row.detected_region;
              return (
                <Space size={4}>
                  <RegionTag region={row.expected_region} />
                  {!matches && row.detected_region && (
                    <>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>→</Typography.Text>
                      <Tooltip title="Region mismatch detected!">
                        <span><RegionTag region={row.detected_region} /></span>
                      </Tooltip>
                    </>
                  )}
                </Space>
              );
            }
          },
          { 
            title: "Exit Info & ISP", 
            key: "exit_info",
            sorter: (left, right) => (left.exit_ip ?? "").localeCompare(right.exit_ip ?? ""),
            render: (_, row) => {
              const locationParts = [row.city, row.asn, row.isp].filter(Boolean);
              return (
                <Space direction="vertical" size={0}>
                  <Typography.Text style={{ fontSize: 13, fontFamily: "monospace", color: "#334155" }}>
                    {row.exit_ip || "—"}
                  </Typography.Text>
                  {locationParts.length > 0 && (
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      {row.isp ? `${row.isp} ` : ""}({[row.city, row.asn].filter(Boolean).join(", ")})
                    </Typography.Text>
                  )}
                </Space>
              );
            }
          },
          { 
            title: "Latency", 
            dataIndex: "latency_ms", 
            width: 100,
            sorter: (left, right) => left.latency_ms - right.latency_ms,
            render: (val) => getLatencyTag(val)
          },
          { 
            title: "Status", 
            key: "status",
            width: 110,
            sorter: (left, right) => (left.disabled === right.disabled ? (left.healthy === right.healthy ? 0 : left.healthy ? -1 : 1) : left.disabled ? 1 : -1),
            render: (_, row) => {
              if (row.disabled) {
                return <Badge status="default" text={<span style={{ color: "#94a3b8" }}>Disabled</span>} />;
              }
              return row.healthy ? (
                <Badge status="success" text={<span style={{ color: "#10b981", fontWeight: 500 }}>Healthy</span>} />
              ) : (
                <Badge status="error" text={<span style={{ color: "#f43f5e", fontWeight: 500 }}>Unhealthy</span>} />
              );
            }
          },
          { 
            title: "Last Check", 
            dataIndex: "last_check_at",
            sorter: (left, right) => new Date(left.last_check_at ?? 0).getTime() - new Date(right.last_check_at ?? 0).getTime(), 
            render: (val) => <DateTimeText value={val} /> 
          },
          {
            title: "Actions",
            key: "actions",
            width: 100,
            render: (_, row) => {
              const menuItems: MenuProps["items"] = [
                {
                  key: "detail",
                  label: "View details",
                  icon: <EyeOutlined />,
                  onClick: () => setSelectedID(row.id)
                },
                {
                  key: "check",
                  label: "Check now",
                  icon: <PlayCircleOutlined style={{ color: "#0d9488" }} />,
                  onClick: () => checkMutation.mutate(row.id)
                },
                {
                  key: "toggle",
                  label: row.disabled ? "Enable node" : "Disable node",
                  icon: row.disabled ? <CheckOutlined style={{ color: "#10b981" }} /> : <StopOutlined style={{ color: "#eab308" }} />,
                  onClick: () => disableMutation.mutate({ id: row.id, disabled: !row.disabled })
                },
                {
                  type: "divider"
                },
                {
                  key: "delete",
                  danger: true,
                  label: (
                    <ConfirmButton 
                      title="Delete node?" 
                      danger 
                      type="text"
                      style={{ padding: 0, height: "auto", color: "#f43f5e" }}
                      onConfirm={() => deleteNode(row.id).then(refresh)}
                    >
                      Delete node
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
        open={selectedID !== null} 
        onClose={() => setSelectedID(null)} 
        width={500} 
        title={
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ width: 8, height: 8, borderRadius: "50%", background: detail?.healthy ? "#10b981" : "#f43f5e" }} />
            <span>Node Metadata: {detail?.tag}</span>
          </div>
        }
        style={{ backdropFilter: "blur(8px)" }}
      >
        <JsonViewer value={detail} />
      </Drawer>
    </PageContainer>
  );
}
