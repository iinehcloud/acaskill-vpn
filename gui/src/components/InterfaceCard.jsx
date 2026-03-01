const TYPE_ICONS = { WiFi: "", Ethernet: "", Mobile: "", Hotspot: "", Other: "" };
const TYPE_COLORS = { WiFi: "#3b9eff", Ethernet: "#22c55e", Mobile: "#f97316", Hotspot: "#a855f7", Other: "#94a3b8" };

export default function InterfaceCard({ iface, tunnel, onToggle }) {
  const isActive = iface.isActive || tunnel?.isConnected;
  const latency = tunnel?.latencyMs;
  const icon = TYPE_ICONS[iface.type] || "";
  const color = TYPE_COLORS[iface.type] || "#94a3b8";

  return (
    <div className={`iface-card ${isActive ? "iface-active" : ""}`} style={{ "--iface-color": color }}>
      <div className="iface-header">
        <div className="iface-icon">{icon}</div>
        <div className="iface-info">
          <span className="iface-name">{iface.friendlyName || iface.name}</span>
          <span className="iface-type">{iface.type}</span>
        </div>
        <button className={`iface-toggle ${isActive ? "toggle-on" : "toggle-off"}`} onClick={onToggle}>
          <span className="toggle-thumb" />
        </button>
      </div>
      <div className="iface-stats">
        {iface.ip && <span className="iface-stat"><span className="stat-dot" style={{ background: color }} />{String(iface.ip)}</span>}
        {isActive && latency > 0 && <span className="iface-stat"><span className="stat-dot ping" />{Math.round(latency)}ms</span>}
        {isActive && <span className="iface-badge">BONDED</span>}
      </div>
    </div>
  );
}
