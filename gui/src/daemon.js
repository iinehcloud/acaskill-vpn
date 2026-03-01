let msgCounter = 0;

export async function daemonCall(type, payload = null) {
  const id = `msg-${++msgCounter}`;
  const { invoke } = await import("@tauri-apps/api/core");
  try {
    const result = await invoke("daemon_call", {
      msgType: type,
      payload: payload ? JSON.stringify(payload) : null,
    });
    return JSON.parse(result);
  } catch (err) {
    throw new Error(err);
  }
}

export async function getStatus() { return daemonCall("GET_STATUS"); }
export async function getInterfaces() { return daemonCall("GET_INTERFACES"); }
export async function connectInterface(name) { return daemonCall("CONNECT_INTERFACE", { interfaceName: name }); }
export async function disconnectInterface(name) { return daemonCall("DISCONNECT_INTERFACE", { interfaceName: name }); }
export async function connectAll() { return daemonCall("CONNECT_ALL"); }
export async function disconnectAll() { return daemonCall("DISCONNECT_ALL"); }

export function startPolling(callback, intervalMs = 1000) {
  const id = setInterval(async () => {
    try { callback(null, await getStatus()); }
    catch (err) { callback(err, null); }
  }, intervalMs);
  return () => clearInterval(id);
}
