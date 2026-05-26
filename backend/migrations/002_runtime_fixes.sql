CREATE UNIQUE INDEX IF NOT EXISTS idx_node_addr_v2 ON proxy_nodes(protocol, host, port);

UPDATE users
SET total_requests = 0
WHERE total_requests IS NULL;

UPDATE proxy_nodes
SET upload_bytes = 0
WHERE upload_bytes IS NULL;

UPDATE proxy_nodes
SET download_bytes = 0
WHERE download_bytes IS NULL;

UPDATE proxy_nodes
SET total_requests = 0
WHERE total_requests IS NULL;
