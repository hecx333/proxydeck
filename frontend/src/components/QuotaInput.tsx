import React, { useState, useEffect } from "react";
import { InputNumber, Select, Space } from "antd";

export function QuotaInput({ 
  value, 
  onChange 
}: { 
  value?: number; 
  onChange?: (bytes: number) => void;
}) {
  const [amount, setAmount] = useState<number | null>(null);
  const [unit, setUnit] = useState<"B" | "KB" | "MB" | "GB" | "TB">("GB");

  useEffect(() => {
    if (value === undefined || value === null) {
      setAmount(null);
      return;
    }
    if (value === 0) {
      setAmount(0);
      setUnit("GB");
      return;
    }

    const multipliers: Record<"TB" | "GB" | "MB" | "KB" | "B", number> = {
      TB: 1024 * 1024 * 1024 * 1024,
      GB: 1024 * 1024 * 1024,
      MB: 1024 * 1024,
      KB: 1024,
      B: 1
    };

    const units: ("TB" | "GB" | "MB" | "KB" | "B")[] = ["TB", "GB", "MB", "KB", "B"];
    for (const u of units) {
      const mult = multipliers[u];
      const ratio: number = value / mult;
      if (value >= mult && (value % mult === 0 || Number(ratio.toFixed(2)) * mult === value)) {
        setAmount(Number(ratio.toFixed(2)));
        setUnit(u);
        return;
      }
    }
    
    setAmount(value);
    setUnit("B");
  }, [value]);

  const triggerChange = (newAmount: number | null, newUnit: "B" | "KB" | "MB" | "GB" | "TB") => {
    if (!onChange) return;
    if (newAmount === null) {
      onChange(0);
      return;
    }
    const multipliers = {
      B: 1,
      KB: 1024,
      MB: 1024 * 1024,
      GB: 1024 * 1024 * 1024,
      TB: 1024 * 1024 * 1024 * 1024
    };
    onChange(Math.round(newAmount * multipliers[newUnit]));
  };

  return (
    <Space.Compact style={{ width: "100%" }}>
      <InputNumber
        value={amount}
        onChange={(valueAmount: number | null) => {
          setAmount(valueAmount);
          triggerChange(valueAmount, unit);
        }}
        placeholder="e.g. 5"
        style={{ width: "70%" }}
        min={0}
        size="large"
      />
      <Select
        value={unit}
        onChange={(unitSelected: "B" | "KB" | "MB" | "GB" | "TB") => {
          setUnit(unitSelected);
          triggerChange(amount, unitSelected);
        }}
        style={{ width: "30%" }}
        size="large"
        options={[
          { label: "Bytes", value: "B" },
          { label: "KB", value: "KB" },
          { label: "MB", value: "MB" },
          { label: "GB", value: "GB" },
          { label: "TB", value: "TB" }
        ]}
      />
    </Space.Compact>
  );
}
