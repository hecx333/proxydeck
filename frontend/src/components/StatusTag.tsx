import { Tag } from "antd";

export function StatusTag({ ok, okText = "Enabled", badText = "Disabled" }: { ok: boolean; okText?: string; badText?: string }) {
  return <Tag color={ok ? "green" : "red"}>{ok ? okText : badText}</Tag>;
}
