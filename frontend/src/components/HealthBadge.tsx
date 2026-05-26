import { Badge } from "antd";

export function HealthBadge({ healthy }: { healthy: boolean }) {
  return <Badge status={healthy ? "success" : "error"} text={healthy ? "Healthy" : "Unhealthy"} />;
}
