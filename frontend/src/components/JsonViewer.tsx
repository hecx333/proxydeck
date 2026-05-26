export function JsonViewer({ value }: { value: unknown }) {
  return (
    <pre style={{ margin: 0, padding: 16, background: "#0f172a", color: "#e2e8f0", borderRadius: 12, overflow: "auto" }}>
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}
