import client from "./client";

export async function listUsers() {
  const { data } = await client.get("/users");
  return data.items;
}

export async function createUser(payload: Record<string, unknown>) {
  const { data } = await client.post("/users", payload);
  return data;
}

export async function updateUser(id: number, payload: Record<string, unknown>) {
  const { data } = await client.put(`/users/${id}`, payload);
  return data;
}

export async function resetUserPassword(id: number, password: string) {
  const { data } = await client.put(`/users/${id}`, { password });
  return data;
}

export async function deleteUser(id: number) {
  const { data } = await client.delete(`/users/${id}`);
  return data;
}
