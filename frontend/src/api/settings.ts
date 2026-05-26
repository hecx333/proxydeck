import client from "./client";

export async function getSettings() {
  const { data } = await client.get("/settings");
  return data;
}
