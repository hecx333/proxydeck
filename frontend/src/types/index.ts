export interface User {
  id: number;
  uid: string;
  enabled: boolean;
  quota_bytes: number;
  used_bytes: number;
  total_requests: number;
  max_concurrency: number;
  current_concurrency: number;
  expired_at?: string | null;
  remark?: string;
  created_at: string;
}

export interface Subscription {
  id: number;
  name: string;
  type: string;
  url: string;
  enabled: boolean;
  sync_interval_seconds: number;
  last_sync_at?: string | null;
  last_sync_status?: "ok" | "error" | "";
  last_sync_detail?: string;
  remark?: string;
  node_count?: number;
}

export interface SubscriptionSyncResult {
  ok: boolean;
  imported_count: number;
  healthcheck_started?: boolean;
}

export interface ProxyNode {
  id: number;
  tag: string;
  raw_tag?: string;
  protocol: string;
  host: string;
  port: number;
  expected_region: string;
  detected_region: string;
  city: string;
  asn: string;
  isp?: string;
  org?: string;
  exit_ip: string;
  latency_ms: number;
  healthy: boolean;
  disabled: boolean;
  upload_bytes?: number;
  download_bytes?: number;
  total_requests?: number;
  last_check_at?: string | null;
}

export interface ManualImportNode {
  protocol?: string;
  host: string;
  port: number;
  username?: string;
  password?: string;
  tag?: string;
}

export interface NodeImportResult {
  ok: boolean;
  imported_count: number;
  healthcheck_started?: boolean;
}

export interface AuditLog {
  id: number;
  operator: string;
  action: string;
  target_type: string;
  target_id: string;
  detail: string;
  created_at: string;
}
