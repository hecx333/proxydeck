CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  enabled NUMERIC DEFAULT 1,
  quota_bytes INTEGER DEFAULT 0,
  used_bytes INTEGER DEFAULT 0,
  total_requests INTEGER DEFAULT 0,
  max_concurrency INTEGER DEFAULT 0,
  expired_at DATETIME,
  remark TEXT,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_uid ON users(uid);

CREATE TABLE IF NOT EXISTS subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  url TEXT NOT NULL,
  enabled NUMERIC DEFAULT 1,
  sync_interval_seconds INTEGER DEFAULT 0,
  last_sync_at DATETIME,
  remark TEXT,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS proxy_nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  node_key TEXT NOT NULL,
  protocol TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL,
  upstream_username_enc TEXT,
  upstream_password_enc TEXT,
  tls_enabled NUMERIC DEFAULT 0,
  server_name TEXT,
  tag TEXT,
  expected_region TEXT,
  detected_region TEXT,
  city TEXT,
  country TEXT,
  asn TEXT,
  org TEXT,
  isp TEXT,
  exit_ip TEXT,
  latency_ms INTEGER DEFAULT 0,
  healthy NUMERIC DEFAULT 0,
  disabled NUMERIC DEFAULT 0,
  fail_count INTEGER DEFAULT 0,
  last_check_at DATETIME,
  subscription_id INTEGER DEFAULT 0,
  raw_json TEXT,
  upload_bytes INTEGER DEFAULT 0,
  download_bytes INTEGER DEFAULT 0,
  total_requests INTEGER DEFAULT 0,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_nodes_node_key ON proxy_nodes(node_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_node_addr ON proxy_nodes(host, port);
CREATE UNIQUE INDEX IF NOT EXISTS idx_node_addr_v2 ON proxy_nodes(protocol, host, port);

CREATE TABLE IF NOT EXISTS subscription_nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subscription_id INTEGER,
  node_id INTEGER,
  raw_tag TEXT,
  alias_tag TEXT,
  created_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_subscription_nodes_subscription_id ON subscription_nodes(subscription_id);
CREATE INDEX IF NOT EXISTS idx_subscription_nodes_node_id ON subscription_nodes(node_id);

CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operator TEXT,
  action TEXT,
  target_type TEXT,
  target_id TEXT,
  detail TEXT,
  created_at DATETIME
);
