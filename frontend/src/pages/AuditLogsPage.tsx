import { useQuery } from "@tanstack/react-query";
import { Select, Space, Table, Button, Input, Row, Col, Typography, Tag } from "antd";
import { 
  FilterOutlined, 
  SyncOutlined,
  SearchOutlined,
  UserOutlined,
  PlayCircleOutlined,
  HistoryOutlined
} from "@ant-design/icons";
import { useState } from "react";
import { listAuditLogs } from "../api/audit";
import { DateTimeText } from "../components/DateTimeText";
import { PageContainer } from "../components/PageContainer";
import { AuditLog } from "../types";

export function AuditLogsPage() {
  const [keyword, setKeyword] = useState("");
  const [operator, setOperator] = useState("");
  const [action, setAction] = useState("");
  
  const [queryKeyword, setQueryKeyword] = useState("");
  const [queryOperator, setQueryOperator] = useState("");
  const [queryAction, setQueryAction] = useState("");
  
  const [filtersOpen, setFiltersOpen] = useState(false);

  const { data = [], isLoading, refetch } = useQuery<AuditLog[]>({
    queryKey: ["audit", queryKeyword, queryOperator, queryAction],
    queryFn: () => listAuditLogs({ keyword: queryKeyword, operator: queryOperator, action: queryAction })
  });

  const operators = Array.from(new Set(data.map((item) => item.operator).filter(Boolean)));
  const actions = Array.from(new Set(data.map((item) => item.action).filter(Boolean)));

  const applyFilters = () => {
    setQueryKeyword(keyword);
    setQueryOperator(operator);
    setQueryAction(action);
  };

  const resetFilters = () => {
    setKeyword("");
    setOperator("");
    setAction("");
    setQueryKeyword("");
    setQueryOperator("");
    setQueryAction("");
  };

  // Helper to color code audit actions
  const getActionTag = (act: string) => {
    const a = act.toLowerCase();
    if (a.includes("delete") || a.includes("remove")) return <Tag color="error" bordered={false}>{act}</Tag>;
    if (a.includes("create") || a.includes("add")) return <Tag color="success" bordered={false}>{act}</Tag>;
    if (a.includes("update") || a.includes("edit") || a.includes("toggle")) return <Tag color="warning" bordered={false}>{act}</Tag>;
    return <Tag color="default" bordered={false}>{act}</Tag>;
  };

  return (
    <PageContainer
      title="Audit Logs"
      extra={
        <Space>
          <Button 
            icon={<FilterOutlined />} 
            onClick={() => setFiltersOpen(!filtersOpen)}
            type={filtersOpen ? "primary" : "default"}
          >
            Filters {filtersOpen ? "Open" : ""}
          </Button>
          <Button icon={<SyncOutlined />} onClick={() => refetch()}>
            Refresh
          </Button>
        </Space>
      }
    >
      {/* Collapsible Filter Panel */}
      <div className={`filter-card glass-panel ${filtersOpen ? "expanded" : ""}`} style={{ 
        maxHeight: filtersOpen ? 260 : 0, 
        overflow: "hidden", 
        padding: filtersOpen ? "20px" : "0px",
        marginBottom: filtersOpen ? "20px" : "0px",
        opacity: filtersOpen ? 1 : 0,
        transition: "all 0.3s cubic-bezier(0.4, 0, 0.2, 1)"
      }}>
        {filtersOpen && (
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Row gutter={[16, 16]}>
              <Col xs={24} md={10}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>Search Target / Detail</Typography.Text>
                <Input 
                  placeholder="e.g. node expected region / database settings" 
                  value={keyword} 
                  onChange={(e) => setKeyword(e.target.value)} 
                  prefix={<SearchOutlined style={{ color: "#94a3b8" }} />}
                  style={{ marginTop: 6 }}
                />
              </Col>
              
              <Col xs={24} sm={12} md={7}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>Operator</Typography.Text>
                <Select
                  allowClear
                  value={operator || undefined}
                  onChange={(value) => setOperator(value ?? "")}
                  placeholder="Select Operator"
                  style={{ width: "100%", marginTop: 6 }}
                  options={operators.map((value) => ({ value, label: value }))}
                />
              </Col>
              
              <Col xs={24} sm={12} md={7}>
                <Typography.Text strong style={{ fontSize: 12, color: "#475569" }}>Action Type</Typography.Text>
                <Select
                  allowClear
                  value={action || undefined}
                  onChange={(value) => setAction(value ?? "")}
                  placeholder="Select Action"
                  style={{ width: "100%", marginTop: 6 }}
                  options={actions.map((value) => ({ value, label: value }))}
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
        className="glass-panel animate-fade-in"
        dataSource={data}
        pagination={{ pageSize: 15 }}
        scroll={{ x: "max-content" }}
        columns={[
          { 
            title: "Operator", 
            dataIndex: "operator", 
            width: 140,
            sorter: (left, right) => left.operator.localeCompare(right.operator),
            render: (val) => (
              <Space size={6}>
                <UserOutlined style={{ color: "#94a3b8", fontSize: 13 }} />
                <span style={{ fontWeight: 600, color: "#334155" }}>{val || "System"}</span>
              </Space>
            )
          },
          { 
            title: "Action", 
            dataIndex: "action", 
            width: 150,
            sorter: (left, right) => left.action.localeCompare(right.action),
            render: (val) => getActionTag(val)
          },
          { 
            title: "Target Ref", 
            key: "target_ref",
            width: 180,
            sorter: (left, right) => `${left.target_type}#${left.target_id}`.localeCompare(`${right.target_type}#${right.target_id}`), 
            render: (_, row) => (
              <span style={{ fontFamily: "monospace", fontSize: 12, padding: "2px 6px", background: "rgba(15, 23, 42, 0.05)", borderRadius: 4, color: "#475569" }}>
                {row.target_type}#{row.target_id}
              </span>
            )
          },
          { 
            title: "Change Details", 
            dataIndex: "detail",
            render: (val) => (
              <span style={{ color: "#475569", fontSize: 13 }}>{val || <span style={{ color: "#94a3b8", fontStyle: "italic" }}>No details logged</span>}</span>
            )
          },
          { 
            title: "Timestamp", 
            dataIndex: "created_at",
            width: 180,
            defaultSortOrder: "descend", 
            sorter: (left, right) => new Date(left.created_at).getTime() - new Date(right.created_at).getTime(), 
            render: (val) => (
              <Space size={6}>
                <HistoryOutlined style={{ color: "#94a3b8", fontSize: 12 }} />
                <DateTimeText value={val} />
              </Space>
            )
          }
        ]}
      />
    </PageContainer>
  );
}
