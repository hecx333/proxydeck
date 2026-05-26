import { Tag } from "antd";

export function RegionTag({ region }: { region?: string | null }) {
  return <Tag color="cyan">{region || "-"}</Tag>;
}
