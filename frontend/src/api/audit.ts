import client from "./client";

export async function listAuditLogs(params?: Record<string, unknown>) {
  const { data } = await client.get("/audit-logs", { params });
  return data.items;
}
