use serde::Deserialize;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

#[derive(Deserialize, Clone, Debug, Default)]
pub struct Paths {
    pub config: PathBuf,
    pub status: PathBuf,
    pub root: PathBuf,
}

#[derive(Deserialize, Clone, Debug, Default)]
pub struct Event {
    #[serde(default)]
    pub time: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub detail: String,
}

#[derive(Deserialize, Clone, Debug, Default)]
pub struct Status {
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub message: String,
    #[serde(default)]
    pub last_sync: String,
    #[serde(default)]
    pub progress: Option<serde_json::Value>,
    #[serde(default)]
    pub attention: Vec<serde_json::Value>,
    #[serde(default)]
    pub recent: Vec<Event>,
}

pub fn binary() -> PathBuf {
    let name = if cfg!(windows) { "drivesync.exe" } else { "drivesync" };
    if let Ok(exe) = std::env::current_exe()
        && let Some(dir) = exe.parent()
    {
        let candidate = dir.join(name);
        if candidate.is_file() {
            return candidate;
        }
    }
    PathBuf::from(name)
}

pub struct Output {
    pub ok: bool,
    pub stdout: String,
    pub stderr: String,
}

pub fn run(args: &[&str]) -> Result<Output, String> {
    let output = Command::new(binary())
        .args(args)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot start the drivesync engine: {err}"))?;
    Ok(Output {
        ok: output.status.success(),
        stdout: String::from_utf8_lossy(&output.stdout).into_owned(),
        stderr: String::from_utf8_lossy(&output.stderr).into_owned(),
    })
}

pub fn run_streaming(args: &[&str], input: &str, slot: &Mutex<Option<Child>>, mut line: impl FnMut(String)) -> Result<bool, String> {
    let mut child = Command::new(binary())
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|err| format!("cannot start the drivesync engine: {err}"))?;
    if let Some(mut stdin) = child.stdin.take() {
        stdin.write_all(input.as_bytes()).map_err(|err| err.to_string())?;
    }
    let stderr = child.stderr.take();
    let stdout = child.stdout.take();
    *slot.lock().unwrap_or_else(std::sync::PoisonError::into_inner) = Some(child);
    let errors = thread::spawn(move || {
        let mut lines = Vec::new();
        if let Some(stderr) = stderr {
            for text in BufReader::new(stderr).lines().map_while(Result::ok) {
                lines.push(text);
            }
        }
        lines
    });
    if let Some(stdout) = stdout {
        for text in BufReader::new(stdout).lines().map_while(Result::ok) {
            line(text);
        }
    }
    for text in errors.join().unwrap_or_default() {
        line(text);
    }
    let taken = slot.lock().unwrap_or_else(std::sync::PoisonError::into_inner).take();
    match taken {
        Some(mut child) => Ok(child.wait().map_err(|err| err.to_string())?.success()),
        None => Ok(false),
    }
}

pub fn scoped<'a>(account: &'a str, args: &[&'a str]) -> Vec<&'a str> {
    let mut all = Vec::with_capacity(args.len() + 2);
    if !account.is_empty() {
        all.push("--account");
        all.push(account);
    }
    all.extend_from_slice(args);
    all
}

pub fn paths(account: &str) -> Result<Paths, String> {
    let output = run(&scoped(account, &["paths"]))?;
    if !output.ok {
        return Err(output.stderr.trim().trim_start_matches("drivesync: ").to_string());
    }
    serde_json::from_str(output.stdout.trim()).map_err(|err| format!("unexpected answer from the engine: {err}"))
}

pub fn message(output: &Output) -> String {
    output.stderr.trim().trim_start_matches("drivesync: ").to_string()
}

pub fn read_status(paths: &Paths) -> Option<Status> {
    let data = std::fs::read(&paths.status).ok()?;
    serde_json::from_slice(&data).ok()
}

pub struct Supervisor {
    account: String,
    child: Mutex<Option<Child>>,
    stopping: AtomicBool,
    allowed: AtomicBool,
    kick: AtomicBool,
}

impl Supervisor {
    pub fn new(account: &str) -> Arc<Self> {
        Arc::new(Self {
            account: account.to_string(),
            child: Mutex::new(None),
            stopping: AtomicBool::new(false),
            allowed: AtomicBool::new(true),
            kick: AtomicBool::new(false),
        })
    }

    pub fn spawn_loop(self: &Arc<Self>) {
        let this = Arc::clone(self);
        thread::spawn(move || this.supervise());
    }

    fn supervise(&self) {
        let mut backoff = 2u64;
        while !self.stopping.load(Ordering::SeqCst) {
            if !self.allowed.load(Ordering::SeqCst) {
                thread::sleep(Duration::from_millis(500));
                continue;
            }
            self.kick.store(false, Ordering::SeqCst);
            let spawned = Command::new(binary()).args(scoped(&self.account, &["run"])).stdin(Stdio::null()).stdout(Stdio::null()).spawn();
            match spawned {
                Ok(child) => {
                    *self.lock_child() = Some(child);
                    let success = self.wait_for_child();
                    backoff = if success { 2 } else { (backoff * 2).min(60) };
                }
                Err(err) => {
                    eprintln!("drivesync-gui: cannot start the engine: {err}");
                    backoff = (backoff * 2).min(60);
                }
            }
            for _ in 0..backoff * 2 {
                if self.stopping.load(Ordering::SeqCst) || self.kick.load(Ordering::SeqCst) {
                    break;
                }
                thread::sleep(Duration::from_millis(500));
            }
        }
    }

    fn wait_for_child(&self) -> bool {
        loop {
            {
                let mut guard = self.lock_child();
                let Some(child) = guard.as_mut() else { return false };
                match child.try_wait() {
                    Ok(Some(status)) => {
                        *guard = None;
                        return status.success();
                    }
                    Ok(None) => {}
                    Err(_) => {
                        *guard = None;
                        return false;
                    }
                }
            }
            thread::sleep(Duration::from_millis(500));
        }
    }

    fn lock_child(&self) -> std::sync::MutexGuard<'_, Option<Child>> {
        self.child.lock().unwrap_or_else(std::sync::PoisonError::into_inner)
    }

    fn kill_child(&self) {
        if let Some(child) = self.lock_child().as_mut() {
            let _ = child.kill();
        }
    }

    pub fn hold(&self) {
        self.allowed.store(false, Ordering::SeqCst);
    }

    pub fn stop(&self) {
        self.stopping.store(true, Ordering::SeqCst);
        self.kill_child();
    }
}
