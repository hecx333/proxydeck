import { PropsWithChildren, CSSProperties } from "react";
import { Button, Popconfirm } from "antd";

export function ConfirmButton({ 
  title, 
  onConfirm, 
  danger, 
  children,
  type,
  style
}: PropsWithChildren<{ 
  title: string; 
  onConfirm: () => void; 
  danger?: boolean;
  type?: "default" | "primary" | "dashed" | "link" | "text";
  style?: CSSProperties;
}>) {
  return (
    <Popconfirm title={title} onConfirm={onConfirm}>
      <Button danger={danger} type={type} style={style}>{children}</Button>
    </Popconfirm>
  );
}
