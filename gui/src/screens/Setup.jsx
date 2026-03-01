import { useState } from "react";
import { daemonCall } from "../daemon";

export default function Setup({ onComplete }) {
  const [licenseKey, setLicenseKey] = useState("");
  const [deviceName, setDeviceName] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [done, setDone] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!licenseKey.trim()) { setError("Please enter a license key"); return; }
    if (!deviceName.trim()) { setError("Please enter a device name"); return; }
    setLoading(true); setError(null);
    try {
      await daemonCall("SET_LICENSE", { licenseKey: licenseKey.trim().toUpperCase(), deviceName: deviceName.trim() });
      setDone(true);
      setTimeout(onComplete, 1500);
    } catch (err) {
      setError(err.message || "Failed to activate. Check your key and try again.");
    } finally { setLoading(false); }
  };

  return (
    <div className="setup-screen">
      <div className="setup-card">
        <div className="setup-logo">
          <span className="logo-mark large"></span>
          <h1>AcaSkill<strong>VPN</strong></h1>
          <p className="setup-tagline">Multi-network bonding for Nigeria</p>
        </div>
        {done ? (
          <div className="setup-success">
            <div className="success-icon"></div>
            <h2>Activated!</h2>
            <p>Setting up your connection...</p>
          </div>
        ) : (
          <form className="setup-form" onSubmit={handleSubmit}>
            <div className="form-group">
              <label>License Key</label>
              <input type="text" placeholder="ACAS-XXXX-XXXX-XXXX-XXXX" value={licenseKey}
                onChange={e => setLicenseKey(e.target.value)} className="form-input" spellCheck={false} />
            </div>
            <div className="form-group">
              <label>Device Name</label>
              <input type="text" placeholder="e.g. My Laptop" value={deviceName}
                onChange={e => setDeviceName(e.target.value)} className="form-input" />
              <span className="form-hint">Helps identify this device in your account</span>
            </div>
            {error && <div className="form-error"> {error}</div>}
            <button type="submit" className={`btn-activate ${loading ? "btn-loading" : ""}`} disabled={loading}>
              {loading ? <><span className="btn-spinner" /> Activating...</> : "Activate License"}
            </button>
            <p className="setup-footer">Need a license? Visit <a href="https://acaskill.com" target="_blank">acaskill.com</a></p>
          </form>
        )}
      </div>
    </div>
  );
}
