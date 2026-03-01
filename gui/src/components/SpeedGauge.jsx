import { useEffect, useRef, useState } from "react";

export default function SpeedGauge({ bytesRecv, bytesSent, connected }) {
  const prevRecv = useRef(0);
  const prevTime = useRef(Date.now());
  const [dlSpeed, setDlSpeed] = useState(0);

  useEffect(() => {
    const now = Date.now();
    const elapsed = (now - prevTime.current) / 1000;
    if (elapsed > 0 && elapsed < 5) setDlSpeed(Math.max(0, (bytesRecv - prevRecv.current) / elapsed));
    prevRecv.current = bytesRecv;
    prevTime.current = now;
  }, [bytesRecv]);

  const dlMbps = (dlSpeed / (1024 * 1024)).toFixed(2);
  const radius = 70;
  const circumference = 2 * Math.PI * radius;
  const progress = connected ? Math.min(dlSpeed / (20 * 1024 * 1024), 1) : 0;
  const dashOffset = circumference * (1 - progress);

  return (
    <div className="gauge-container">
      <svg className="gauge-svg" viewBox="0 0 180 180" width="180" height="180">
        <circle cx="90" cy="90" r={radius} fill="none" stroke="rgba(255,255,255,0.06)" strokeWidth="10" strokeLinecap="round" />
        <circle cx="90" cy="90" r={radius} fill="none" stroke={connected ? "url(#gaugeGrad)" : "rgba(255,255,255,0.1)"}
          strokeWidth="10" strokeLinecap="round" strokeDasharray={circumference} strokeDashoffset={dashOffset}
          transform="rotate(-90 90 90)" style={{ transition: "stroke-dashoffset 0.8s ease" }} />
        <defs>
          <linearGradient id="gaugeGrad" x1="0%" y1="0%" x2="100%" y2="0%">
            <stop offset="0%" stopColor="#3b9eff" /><stop offset="100%" stopColor="#22d3ee" />
          </linearGradient>
        </defs>
        <text x="90" y="82" textAnchor="middle" className="gauge-speed-value">{connected ? dlMbps : ""}</text>
        <text x="90" y="100" textAnchor="middle" className="gauge-speed-unit">{connected ? "Mbps" : "offline"}</text>
        <text x="90" y="116" textAnchor="middle" className="gauge-speed-label">{connected ? "Download" : ""}</text>
      </svg>
    </div>
  );
}
