import client from "./client";

export async function login(payload: { username: string; password: string }) {
  const { data } = await client.post("/admin/login", payload);
  return data;
}

export async function logout() {
  const { data } = await client.post("/admin/logout");
  return data;
}

export async function me() {
  const { data } = await client.get("/admin/me", { silenceError: true } as any);
  return data;
}
