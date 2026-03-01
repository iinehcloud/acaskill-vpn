export default function StatusBadge({ connected, tunnels }) {
  return (
    <div className={`status-badge ${connected ? "status-on" : "status-off"}`}>
      <span className="status-dot" />
      <span className="status-text">
        {connected ? `${tunnels} bonded` : "disconnected"}
      </span>
    </div>
  );
}
