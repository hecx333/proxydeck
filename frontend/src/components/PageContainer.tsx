import { PropsWithChildren, ReactNode } from "react";
import { Typography } from "antd";

export function PageContainer({ title, extra, children }: PropsWithChildren<{ title: string; extra?: ReactNode }>) {
  return (
    <div className="page-shell">
      <div className="page-header" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            {title}
          </Typography.Title>
        </div>
        {extra}
      </div>
      {children}
    </div>
  );
}
