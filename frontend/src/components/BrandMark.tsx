type BrandMarkProps = {
  size?: number;
  showWordmark?: boolean;
  stacked?: boolean;
  inverse?: boolean;
};

export function BrandMark({ size = 40, showWordmark = false, stacked = false, inverse = false }: BrandMarkProps) {
  const textColor = inverse ? "#f8fafc" : "#0f172a";
  const subColor = inverse ? "rgba(241, 245, 249, 0.72)" : "#64748b";

  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: stacked ? "flex-start" : "center",
        flexDirection: stacked ? "column" : "row",
        gap: stacked ? 10 : 12,
      }}
    >
      <svg width={size} height={size} viewBox="0 0 64 64" fill="none" aria-hidden="true">
        <defs>
          <linearGradient id="proxydeck-gradient" x1="10" y1="8" x2="54" y2="56" gradientUnits="userSpaceOnUse">
            <stop stopColor="#14B8A6" />
            <stop offset="1" stopColor="#2563EB" />
          </linearGradient>
        </defs>
        <rect x="6" y="6" width="52" height="52" rx="18" fill="#0F172A" />
        <rect x="9" y="9" width="46" height="46" rx="15" fill="url(#proxydeck-gradient)" />
        <path
          d="M21 21.5H42.5C46.09 21.5 49 24.41 49 28V35.5C49 42.96 42.96 49 35.5 49H21V21.5Z"
          fill="rgba(255,255,255,0.18)"
        />
        <path
          d="M19 18H34.5C42.51 18 49 24.49 49 32.5C49 40.51 42.51 47 34.5 47H19V18Z"
          fill="rgba(15,23,42,0.78)"
        />
        <path
          d="M27 25H36.5C40.64 25 44 28.36 44 32.5C44 36.64 40.64 40 36.5 40H27V25Z"
          fill="#F8FAFC"
        />
        <path d="M27 29H39" stroke="#14B8A6" strokeWidth="2.8" strokeLinecap="round" />
        <path d="M27 35H35.5" stroke="#2563EB" strokeWidth="2.8" strokeLinecap="round" />
        <circle cx="21.5" cy="25.5" r="3.5" fill="#F8FAFC" />
        <circle cx="21.5" cy="39.5" r="3.5" fill="#0F172A" stroke="#F8FAFC" strokeWidth="2" />
      </svg>
      {showWordmark ? (
        <div style={{ lineHeight: 1 }}>
          <div style={{ color: textColor, fontSize: stacked ? 26 : 21, fontWeight: 700, letterSpacing: "-0.04em" }}>ProxyDeck</div>
          <div style={{ color: subColor, fontSize: stacked ? 11 : 10, fontWeight: 600, letterSpacing: "0.18em", marginTop: 4 }}>
            PROXY GATEWAY
          </div>
        </div>
      ) : null}
    </div>
  );
}
