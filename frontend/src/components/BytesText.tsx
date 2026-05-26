import { formatBytes } from "../utils/format";

export function BytesText({ value }: { value?: number | null }) {
  return <span>{formatBytes(value)}</span>;
}
