import { formatDateTime } from "../utils/format";

export function DateTimeText({ value }: { value?: string | null }) {
  return <span>{formatDateTime(value)}</span>;
}
