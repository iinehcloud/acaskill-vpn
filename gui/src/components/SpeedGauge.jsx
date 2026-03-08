import { useEffect, useRef, useState } from "react";

const MAX_POINTS = 40;
const COLORS = {
  WiFi: "#3b9eff",
  Ethernet: "#22c55e",
  Mobile: "#f97316",
  Hotspot: "#a855f7",
  Other: "#94a3b8",
};

function MiniGraph({ points, color, maxVal }) {
  const w = 100, h = 28;
  if (!points || points.length < 2) return (
    <svg width={w} height={h}>
      <line x1="0" y1={h-1} x2={w} y2={h-1} stroke="rgba(255,255,255,0.06)" strokeWidth="1"/>
    </svg>
  );
  const padded = [...Array(MAX_POINTS - points.length).fill(0), ...points];
  const step = w / (MAX_POINTS - 1);
  const top = Math.max(maxVal, 0.1);
  const scale = (h - 4) / top;
  const pts = padded.map((v, i) => `${(i*step).toFixed(1)},${(h-2-v*scale).toFixed(1)}`).join(" ");
  const area = `0,${h} ${pts} ${w},${h}`;
  const id = `g${color.replace("#","")}`;
  return (
    <svg width={w} height={h} style={{overflow:"visible"}}>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.3"/>
          <stop offset="100%" stopColor={color} stopOpacity="0"/>
        </linearGradient>
      </defs>
      <polygon points={area} fill={`url(#${id})`}/>
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
      <circle cx={((MAX_POINTS-1)*step).toFixed(1)} cy={(h-2-padded[MAX_POINTS-1]*scale).toFixed(1)} r="2" fill={color}/>
    </svg>
  );
}

export default function SpeedGauge({ status, connected }) {
  const [dlSpeed, setDlSpeed] = useState(0);
  const [ulSpeed, setUlSpeed] = useState(0);
  const [history, setHistory] = useState({});

  // All prev state in a single ref — never stale inside effects
  const prevRef = useRef({
    time: 0,
    totalRx: 0,
    totalTx: 0,
    tunnels: {}, // { [name]: { rx, tx } }
  });

  useEffect(() => {
    if (!status) return;

    const now = Date.now();
    const prev = prevRef.current;
    const elapsed = prev.time > 0 ? (now - prev.time) / 1000 : 0;

    // ── Total speed ──────────────────────────────────────────
    let rx = status.totalSpeedRxMbps ?? 0;
    let tx = status.totalSpeedTxMbps ?? 0;
    if (rx === 0 && tx === 0 && elapsed > 0 && elapsed < 5) {
      rx = Math.max(0, ((status.totalBytesRecv ?? 0) - prev.totalRx) * 8 / elapsed / 1_000_000);
      tx = Math.max(0, ((status.totalBytesSent ?? 0) - prev.totalTx) * 8 / elapsed / 1_000_000);
    }
    setDlSpeed(rx);
    setUlSpeed(tx);

    // ── Per-tunnel speed + history ────────────────────────────
    const nextTunnelPrev = { ...prev.tunnels };
    if (status.tunnels) {
      const updates = {};
      for (const t of status.tunnels) {
        const name = t.interfaceName;
        let tRx = t.speedRxMbps ?? 0;
        let tTx = t.speedTxMbps ?? 0;
        if (tRx === 0 && tTx === 0 && elapsed > 0 && elapsed < 5) {
          const pt = prev.tunnels[name] || { rx: 0, tx: 0 };
          tRx = Math.max(0, ((t.bytesRecv ?? 0) - pt.rx) * 8 / elapsed / 1_000_000);
          tTx = Math.max(0, ((t.bytesSent ?? 0) - pt.tx) * 8 / elapsed / 1_000_000);
        }
        nextTunnelPrev[name] = { rx: t.bytesRecv ?? 0, tx: t.bytesSent ?? 0 };
        updates[name] = { tRx, tTx, type: t.type };
      }

      setHistory(h => {
        const next = { ...h };
        for (const [name, { tRx, tTx, type }] of Object.entries(updates)) {
          const existing = next[name] || { rx: [], tx: [] };
          next[name] = {
            rx: [...existing.rx.slice(-(MAX_POINTS-1)), tRx],
            tx: [...existing.tx.slice(-(MAX_POINTS-1)), tTx],
            rxNow: tRx,
            txNow: tTx,
            type,
          };
        }
        return next;
      });
    }

    // Update ref AFTER computing deltas
    prevRef.current = {
      time: now,
      totalRx: status.totalBytesRecv ?? 0,
      totalTx: status.totalBytesSent ?? 0,
      tunnels: nextTunnelPrev,
    };
  }, [status]);

  const radius = 70;
  const circumference = 2 * Math.PI * radius;
  const progress = connected ? Math.min(dlSpeed / 100, 1) : 0;
  const dashOffset = circumference * (1 - progress);
  const tunnelEntries = status?.tunnels ?? [];
  const allVals = Object.values(history).flatMap(h => [...(h.rx||[]),...(h.tx||[])]);
  const maxVal = Math.max(0.1, ...allVals);

  return (
    <div style={{display:"flex",flexDirection:"column",alignItems:"center",gap:"6px",width:"100%"}}>
      {/* Gauge */}
      <div className="gauge-container">
        <svg className="gauge-svg" viewBox="0 0 180 180" width="180" height="180">
          <circle cx="90" cy="90" r={radius} fill="none" stroke="rgba(255,255,255,0.06)" strokeWidth="10" strokeLinecap="round"/>
          <circle cx="90" cy="90" r={radius} fill="none"
            stroke={connected ? "url(#gaugeGrad)" : "rgba(255,255,255,0.1)"}
            strokeWidth="10" strokeLinecap="round"
            strokeDasharray={circumference} strokeDashoffset={dashOffset}
            transform="rotate(-90 90 90)"
            style={{transition:"stroke-dashoffset 0.8s ease"}}/>
          <defs>
            <linearGradient id="gaugeGrad" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#3b9eff"/><stop offset="100%" stopColor="#22d3ee"/>
            </linearGradient>
          </defs>
          <text x="90" y="80" textAnchor="middle" className="gauge-speed-value">{connected ? dlSpeed.toFixed(2) : ""}</text>
          <text x="90" y="98" textAnchor="middle" className="gauge-speed-unit">{connected ? "Mbps" : "offline"}</text>
          <text x="90" y="114" textAnchor="middle" className="gauge-speed-label">{connected ? "↓ Download" : ""}</text>
          {connected && ulSpeed > 0.01 && (
            <text x="90" y="130" textAnchor="middle" style={{fill:"#22c55e",fontSize:"10px",fontFamily:"inherit"}}>
              ↑ {ulSpeed.toFixed(2)} Mbps
            </text>
          )}
        </svg>
      </div>

      {/* Per-tunnel compact graphs */}
      {connected && tunnelEntries.length > 0 && (
        <div style={{width:"100%",padding:"0 14px",boxSizing:"border-box",display:"flex",flexDirection:"column",gap:"4px"}}>
          <div style={{fontSize:"9px",color:"rgba(255,255,255,0.28)",textTransform:"uppercase",letterSpacing:"0.1em",marginBottom:"1px"}}>
            Tunnel Throughput
          </div>
          {tunnelEntries.map(t => {
            const color = COLORS[t.type] || COLORS.Other;
            const th = history[t.interfaceName] || {};
            return (
              <div key={t.interfaceName} style={{
                background:"rgba(255,255,255,0.03)",borderRadius:"6px",padding:"4px 8px",
                display:"flex",alignItems:"center",gap:"8px",borderLeft:`2px solid ${color}`,
              }}>
                <div style={{width:"60px",flexShrink:0}}>
                  <div style={{fontSize:"10px",color:"rgba(255,255,255,0.75)",fontWeight:600,lineHeight:1.3}}>{t.interfaceName}</div>
                  <div style={{fontSize:"9px",color,fontFamily:"monospace",lineHeight:1.3}}>
                    ↓{(th.rxNow??0).toFixed(1)} ↑{(th.txNow??0).toFixed(1)}
                  </div>
                </div>
                <MiniGraph points={th.rx} color={color} maxVal={maxVal}/>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
