use std::io::{BufRead, BufReader, Write};
use std::net::TcpStream;
use std::sync::atomic::{AtomicU64, Ordering};

static MSG_COUNTER: AtomicU64 = AtomicU64::new(1);

#[tauri::command]
fn daemon_call(msg_type: String, payload: Option<String>) -> Result<String, String> {
    let stream = TcpStream::connect("127.0.0.1:47821")
        .map_err(|e| format!("Cannot connect to daemon: {}", e))?;
    stream.set_read_timeout(Some(std::time::Duration::from_secs(10))).map_err(|e| e.to_string())?;
    let mut writer = stream.try_clone().map_err(|e| e.to_string())?;
    let reader = BufReader::new(stream);
    let id = format!("gui-{}", MSG_COUNTER.fetch_add(1, Ordering::SeqCst));
    let request = match payload {
        Some(p) => format!(r#"{{"id":"{}","type":"{}","payload":{}}}"#, id, msg_type, p),
        None    => format!(r#"{{"id":"{}","type":"{}"}}"#, id, msg_type),
    };
    writeln!(writer, "{}", request).map_err(|e| format!("Send: {}", e))?;
    let response = reader.lines().next()
        .ok_or("No response".to_string())?
        .map_err(|e| format!("Read: {}", e))?;
    if response.contains(r#""type":"ERROR""#) {
        if let Some(start) = response.find(r#""message":""#) {
            let s = start + 12;
            if let Some(end) = response[s..].find('"') { return Err(response[s..s+end].to_string()); }
        }
        return Err("Daemon error".to_string());
    }
    if let Some(start) = response.find(r#""payload":"#) {
        let ps = start + 10;
        if let Some(last) = response.rfind('}') {
            return Ok(response[ps..last].to_string());
        }
    }
    Ok(response)
}

#[tauri::command]
fn check_daemon() -> bool { TcpStream::connect("127.0.0.1:47821").is_ok() }

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![daemon_call, check_daemon])
        .run(tauri::generate_context!())
        .expect("error running app");
}
