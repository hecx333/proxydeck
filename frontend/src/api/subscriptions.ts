import { SubscriptionSyncResult } from "../types";
import client from "./client";

export async function listSubscriptions() {
  const { data } = await client.get("/subscriptions");
  return data.items;
}

export async function createSubscription(payload: Record<string, unknown>) {
  const { data } = await client.post("/subscriptions", payload);
  return data;
}

export async function updateSubscription(id: number, payload: Record<string, unknown>) {
  const { data } = await client.put(`/subscriptions/${id}`, payload);
  return data;
}

export async function deleteSubscription(id: number) {
  const { data } = await client.delete(`/subscriptions/${id}`);
  return data;
}

export async function syncSubscription(id: number) {
  const { data } = await client.post<SubscriptionSyncResult>(`/subscriptions/${id}/sync`);
  return data;
}
