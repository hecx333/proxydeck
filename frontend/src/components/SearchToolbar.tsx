import { Button, Input, Space } from "antd";

export function SearchToolbar({
  value,
  onChange,
  onSearch,
  placeholder = "Search"
}: {
  value: string;
  onChange: (value: string) => void;
  onSearch: () => void;
  placeholder?: string;
}) {
  return (
    <Space wrap>
      <Input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} style={{ width: 240 }} />
      <Button type="primary" onClick={onSearch}>
        Search
      </Button>
    </Space>
  );
}
