import client from "./client";

export async function listNodes(params?: Record<string, unknown>) {
  const { data } = await client.get("/nodes", { params });
  return data.items;
}

export async function getNode(id: number) {
  const { data } = await client.get(`/nodes/${id}`);
  return data;
}

export async function checkNode(id: number) {
  const { data } = await client.post(`/nodes/${id}/check`);
  return data;
}

export async function toggleNodeDisabled(id: number, disabled: boolean) {
  const { data } = await client.put(`/nodes/${id}/disable`, { disabled });
  return data;
}

export async function deleteNode(id: number) {
  const { data } = await client.delete(`/nodes/${id}`);
  return data;
}
