import client from "./client";

export interface OverviewErrorLog {
  action: string;
  target: string;
  detail: string;
  created_at: string;
}

export interface OverviewStats {
  total_users: number;
  active_users: number;
  total_nodes: number;
  healthy_nodes: number;
  unhealthy_nodes: number;
  total_traffic: number;
  active_connections: number;
  total_requests: number;
  today_traffic: number;
  region_counts: Record<string, number>;
  recent_errors: OverviewErrorLog[];
}

export async function getOverview() {
  const { data } = await client.get<OverviewStats>("/stats/overview");
  return data;
}

export async function getUserStats() {
  const { data } = await client.get("/stats/users");
  return data.items;
}

export async function getNodeStats() {
  const { data } = await client.get("/stats/nodes");
  return data.items;
}

export async function getTrafficStats() {
  const { data } = await client.get("/stats/traffic");
  return data;
}
