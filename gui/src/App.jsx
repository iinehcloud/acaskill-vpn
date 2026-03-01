import { useState, useEffect } from "react";
import Dashboard from "./screens/Dashboard";
import Setup from "./screens/Setup";
import { daemonCall } from "./daemon";

export default function App() {
  const [screen, setScreen] = useState("loading");

  useEffect(() => {
    daemonCall("GET_STATUS")
      .then((status) => {
        // If we got a status back, daemon is running
        // Check if provisioned by seeing if tunnelCount is meaningful
        // or just go to dashboard — license check happens on connect
        setScreen("dashboard");
      })
      .catch(() => {
        // Daemon not reachable OR not provisioned — show setup
        setScreen("setup");
      });
  }, []);

  if (screen === "loading") return (
    <div className="loading-screen">
      <div className="spinner" />
      <p>Connecting to daemon...</p>
    </div>
  );

  if (screen === "setup") return <Setup onComplete={() => setScreen("dashboard")} />;
  return <Dashboard onSetup={() => setScreen("setup")} />;
}
