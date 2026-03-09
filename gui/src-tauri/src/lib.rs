use std::io::{BufRead, BufReader, Write};
use std::net::TcpStream;
use std::sync::atomic::{AtomicU64, Ordering};
use std::path::PathBuf;

static MSG_COUNTER: AtomicU64 = AtomicU64::new(1);

#[tauri::command]
fn daemon_call(msg_type: String, payload: Option<String>) -> Result<String, String> {
    let stream = TcpStream::connect("127.0.0.1:47821")
        .map_err(|e| format!("Cannot connect to daemon: {}", e))?;
    stream.set_read_timeout(Some(std::time::Duration::from_secs(30))).map_err(|e| e.to_string())?;
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

/// Find the daemon exe next to the GUI exe
fn daemon_path() -> Option<PathBuf> {
    let exe = std::env::current_exe().ok()?;
    let dir = exe.parent()?;
    let p = dir.join("acaskill-daemon.exe");
    if p.exists() { Some(p) } else { None }
}

fn is_service_installed() -> bool {
    let out = std::process::Command::new("sc.exe")
        .args(["query", "AcaSkillVPN"])
        .output();
    match out {
        Ok(o) => String::from_utf8_lossy(&o.stdout).contains("AcaSkillVPN"),
        Err(_) => false,
    }
}

fn is_service_running() -> bool {
    let out = std::process::Command::new("sc.exe")
        .args(["query", "AcaSkillVPN"])
        .output();
    match out {
        Ok(o) => String::from_utf8_lossy(&o.stdout).contains("RUNNING"),
        Err(_) => false,
    }
}

fn install_and_start_daemon() {
    let Some(daemon) = daemon_path() else {
        eprintln!("[setup] daemon exe not found next to GUI");
        return;
    };

    // Already connected — nothing to do
    if TcpStream::connect("127.0.0.1:47821").is_ok() {
        eprintln!("[setup] daemon already running");
        return;
    }

    // Install service if not present
    if !is_service_installed() {
        eprintln!("[setup] installing daemon service...");
        let status = std::process::Command::new(&daemon)
            .arg("install")
            .status();
        match status {
            Ok(s) if s.success() => eprintln!("[setup] service installed"),
            Ok(s) => eprintln!("[setup] install exited: {}", s),
            Err(e) => eprintln!("[setup] install error: {}", e),
        }
    }

    // Start service if not running
    if !is_service_running() {
        eprintln!("[setup] starting daemon service...");
        let _ = std::process::Command::new("sc.exe")
            .args(["start", "AcaSkillVPN"])
            .status();

        // Wait up to 10s for daemon to come up
        for _ in 0..20 {
            std::thread::sleep(std::time::Duration::from_millis(500));
            if TcpStream::connect("127.0.0.1:47821").is_ok() {
                eprintln!("[setup] daemon is up");
                return;
            }
        }
        eprintln!("[setup] daemon did not start in time");
    }
}

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|_app| {
            // Spawn in background so GUI doesn't block on service install
            std::thread::spawn(install_and_start_daemon);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![daemon_call, check_daemon])
        .run(tauri::generate_context!())
        .expect("error running app");
}
