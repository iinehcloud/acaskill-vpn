import { useState, useEffect } from "react";
import { getStatus, getInterfaces, connectInterface, disconnectInterface, connectAll, disconnectAll, startPolling } from "../daemon";
import InterfaceCard from "../components/InterfaceCard";
import SpeedGauge from "../components/SpeedGauge";
import StatusBadge from "../components/StatusBadge";

function formatBytes(b) {
  if (!b) return "0 B";
  const units = ["B","KB","MB","GB"]; let i = 0, v = b;
  while (v >= 1024 && i < 3) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${units[i]}`;
}

export default function Dashboard() {
  const [status, setStatus] = useState(null);
  const [interfaces, setInterfaces] = useState([]);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => { getInterfaces().then(setInterfaces).catch(e => setError(e.message)); }, []);
  useEffect(() => {
    const stop = startPolling((err, data) => { if (err) setError("Lost connection to daemon"); else { setStatus(data); setError(null); } }, 1000);
    return stop;
  }, []);

  const isConnected = status?.isConnected ?? false;
  const activeTunnels = status?.activeTunnels ?? 0;
  const totalTunnels = status?.tunnelCount ?? 0;
  const latency = status?.combinedLatencyMs ?? 0;
  const bytesSent = status?.totalBytesSent ?? 0;
  const bytesRecv = status?.totalBytesRecv ?? 0;

  const handleConnectAll = async () => { setConnecting(true); try { await connectAll(); } catch(e) { setError(e.message); } finally { setConnecting(false); } };
  const handleDisconnectAll = async () => { setConnecting(true); try { await disconnectAll(); } catch(e) { setError(e.message); } finally { setConnecting(false); } };
  const handleToggle = async (iface) => {
    try {
      if (iface.isActive) await disconnectInterface(iface.name); else await connectInterface(iface.name);
      setInterfaces(await getInterfaces());
    } catch(e) { setError(e.message); }
  };

  return (
    <div className="dashboard">
      <header className="dash-header">
        <div className="logo"><span className="logo-mark"></span><span className="logo-text">AcaSkill<strong>VPN</strong></span></div>
        <StatusBadge connected={isConnected} tunnels={activeTunnels} />
      </header>
      <section className="hero-section">
        <SpeedGauge bytesRecv={bytesRecv} bytesSent={bytesSent} connected={isConnected} />
        <div className="hero-stats">
          <div className="stat"><span className="stat-value">{activeTunnels}/{totalTunnels}</span><span className="stat-label">Interfaces</span></div>
          <div className="stat-divider" />
          <div className="stat"><span className="stat-value">{latency > 0 ? `${Math.round(latency)}ms` : ""}</span><span className="stat-label">Latency</span></div>
          <div className="stat-divider" />
          <div className="stat"><span className="stat-value">EU</span><span className="stat-label">Server</span></div>
        </div>
        <button className={`btn-connect ${isConnected ? "btn-disconnect" : ""}`} onClick={isConnected ? handleDisconnectAll : handleConnectAll} disabled={connecting}>
          {connecting ? <><span className="btn-spinner" />{isConnected ? " Disconnecting..." : " Connecting..."}</> : isConnected ? "Disconnect" : "Connect All"}
        </button>
      </section>
      {error && <div className="error-bar"><span> {error}</span><button onClick={() => setError(null)}></button></div>}
      <section className="interfaces-section">
        <h2 className="section-title">Network Interfaces</h2>
        <p className="section-sub">Select which connections to bond together</p>
        <div className="interfaces-grid">
          {interfaces.length === 0
            ? <div className="no-interfaces"><span></span><p>Scanning for interfaces...</p></div>
            : interfaces.map(iface => <InterfaceCard key={iface.name} iface={iface} tunnel={status?.tunnels?.find(t => t.interfaceName === iface.friendlyName)} onToggle={() => handleToggle(iface)} />)
          }
        </div>
      </section>
      <footer className="dash-footer">
        <span>vpn.acaskill.com</span><span className="footer-dot"></span>
        <span>{formatBytes(bytesRecv)} received</span><span className="footer-dot"></span>
        <span>{formatBytes(bytesSent)} sent</span>
      </footer>
    </div>
  );
}
