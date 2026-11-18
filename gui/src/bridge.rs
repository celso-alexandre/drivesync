#[cxx_qt::bridge]
pub mod qobject {
    unsafe extern "C++" {
        include!("cxx-qt-lib/qstring.h");
        type QString = cxx_qt_lib::QString;
    }

    #[auto_cxx_name]
    extern "RustQt" {
        #[qobject]
        #[qml_element]
        #[qproperty(QString, sync_state)]
        #[qproperty(QString, message)]
        #[qproperty(QString, last_sync)]
        #[qproperty(QString, recent_json)]
        #[qproperty(QString, root)]
        #[qproperty(bool, paused)]
        #[qproperty(bool, start_at_login)]
        #[qproperty(QString, folders_json)]
        #[qproperty(bool, folders_loading)]
        #[qproperty(QString, folders_error)]
        #[qproperty(bool, signing_in)]
        #[qproperty(QString, sign_in_log)]
        type Backend = super::BackendRust;

        #[qsignal]
        fn alert(self: Pin<&mut Backend>, title: QString, body: QString);

        #[qsignal]
        fn signed_in(self: Pin<&mut Backend>);

        #[qinvokable]
        fn start(self: Pin<&mut Backend>);

        #[qinvokable]
        fn sync_now(self: Pin<&mut Backend>);

        #[qinvokable]
        fn set_sync_paused(self: Pin<&mut Backend>, paused: bool);

        #[qinvokable]
        fn load_folders(self: Pin<&mut Backend>);

        #[qinvokable]
        fn save_folders(self: Pin<&mut Backend>, names_json: &QString);

        #[qinvokable]
        fn sign_in(self: Pin<&mut Backend>, client_id: &QString, client_secret: &QString);

        #[qinvokable]
        fn sign_out(self: Pin<&mut Backend>);

        #[qinvokable]
        fn set_login_start(self: Pin<&mut Backend>, enabled: bool);

        #[qinvokable]
        fn shutdown(self: Pin<&mut Backend>);
    }

    impl cxx_qt::Threading for Backend {}
}

use crate::engine::{self, Paths, Supervisor};
use crate::{autostart, settings};
use core::pin::Pin;
use cxx_qt::{CxxQtType, Threading};
use cxx_qt_lib::QString;
use serde_json::Value;
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

pub struct BackendRust {
    sync_state: QString,
    message: QString,
    last_sync: QString,
    recent_json: QString,
    root: QString,
    paused: bool,
    start_at_login: bool,
    folders_json: QString,
    folders_loading: bool,
    folders_error: QString,
    signing_in: bool,
    sign_in_log: QString,
    supervisor: Arc<Supervisor>,
    started: bool,
}

impl Default for BackendRust {
    fn default() -> Self {
        Self {
            sync_state: QString::from("starting"),
            message: QString::default(),
            last_sync: QString::default(),
            recent_json: QString::from("[]"),
            root: QString::default(),
            paused: false,
            start_at_login: false,
            folders_json: QString::from("[]"),
            folders_loading: false,
            folders_error: QString::default(),
            signing_in: false,
            sign_in_log: QString::default(),
            supervisor: Supervisor::new(),
            started: false,
        }
    }
}

type Handle = cxx_qt::CxxQtThread<qobject::Backend>;

const ALERT_KINDS: [&str; 3] = ["conflict", "error", "warning"];

fn now_nanos() -> i64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos() as i64).unwrap_or_default()
}

fn alert_title(kind: &str) -> &'static str {
    match kind {
        "conflict" => "A file changed in two places",
        "error" => "drivesync could not sync something",
        _ => "drivesync needs your attention",
    }
}

fn change_settings(change: impl FnOnce(&mut serde_json::Map<String, Value>)) -> Result<(), String> {
    let paths = engine::paths()?;
    settings::update(&paths.config, change)
}

fn watch(handle: Handle, paths: &'static Paths, supervisor: Arc<Supervisor>) {
    let mut last_status = None;
    let mut last_settings = None;
    let mut newest_seen: Option<String> = None;
    loop {
        let settings_time = std::fs::metadata(&paths.config).and_then(|m| m.modified()).ok();
        if settings_time != last_settings {
            last_settings = settings_time;
            let current = settings::read(&paths.config);
            let paused = current.get("paused").and_then(Value::as_bool).unwrap_or(false);
            let login = current.get("gui_start_at_login").and_then(Value::as_bool).unwrap_or(false);
            let root = current.get("root").and_then(Value::as_str).map(str::to_string).unwrap_or_else(|| paths.root.display().to_string());
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_paused(paused);
                backend.as_mut().set_start_at_login(login);
                backend.as_mut().set_root(QString::from(&root));
            });
        }
        let status_time = std::fs::metadata(&paths.status).and_then(|m| m.modified()).ok();
        if status_time.is_some() && status_time != last_status {
            last_status = status_time;
            if let Some(status) = engine::read_status(paths) {
                if status.state == "signed_out" {
                    supervisor.hold();
                }
                let mut alerts = Vec::new();
                if let Some(seen) = &newest_seen {
                    for event in status.recent.iter().take_while(|e| &e.time > seen) {
                        if ALERT_KINDS.contains(&event.kind.as_str()) {
                            let body = if event.path.is_empty() { event.detail.clone() } else { format!("{}: {}", event.path, event.detail) };
                            alerts.push((alert_title(&event.kind).to_string(), body));
                        }
                    }
                }
                if let Some(first) = status.recent.first() {
                    newest_seen = Some(first.time.clone());
                } else if newest_seen.is_none() {
                    newest_seen = Some(String::new());
                }
                let recent = serde_json::to_string(&status.recent.iter().take(20).map(|e| serde_json::json!({"time": e.time, "kind": e.kind, "path": e.path, "detail": e.detail})).collect::<Vec<_>>()).unwrap_or_else(|_| "[]".into());
                let _ = handle.queue(move |mut backend| {
                    backend.as_mut().set_sync_state(QString::from(&status.state));
                    backend.as_mut().set_message(QString::from(&status.message));
                    backend.as_mut().set_last_sync(QString::from(&status.last_sync));
                    backend.as_mut().set_recent_json(QString::from(&recent));
                    for (title, body) in alerts {
                        backend.as_mut().alert(QString::from(&title), QString::from(&body));
                    }
                });
            }
        }
        std::thread::sleep(Duration::from_millis(700));
    }
}

impl qobject::Backend {
    fn start(mut self: Pin<&mut Self>) {
        if self.rust().started {
            return;
        }
        self.as_mut().rust_mut().started = true;
        let handle = self.qt_thread();
        let supervisor = Arc::clone(&self.rust().supervisor);
        std::thread::spawn(move || match engine::paths() {
            Ok(paths) => {
                supervisor.spawn_loop();
                watch(handle, paths, supervisor);
            }
            Err(err) => {
                let _ = handle.queue(move |mut backend| {
                    backend.as_mut().set_sync_state(QString::from("engine_missing"));
                    backend.as_mut().set_message(QString::from(&err));
                });
            }
        });
    }

    fn sync_now(self: Pin<&mut Self>) {
        if let Err(err) = change_settings(|s| {
            s.insert("sync_requested".into(), Value::from(now_nanos()));
        }) {
            eprintln!("drivesync-gui: {err}");
        }
    }

    fn set_sync_paused(mut self: Pin<&mut Self>, paused: bool) {
        match change_settings(|s| {
            s.insert("paused".into(), Value::Bool(paused));
        }) {
            Ok(()) => self.as_mut().set_paused(paused),
            Err(err) => self.as_mut().set_message(QString::from(&err)),
        }
    }

    fn load_folders(mut self: Pin<&mut Self>) {
        self.as_mut().set_folders_loading(true);
        self.as_mut().set_folders_error(QString::default());
        let handle = self.qt_thread();
        std::thread::spawn(move || {
            let result = engine::run(&["folders", "--json"]).and_then(|out| {
                if out.ok {
                    Ok(out.stdout.trim().to_string())
                } else {
                    Err(out.stderr.trim().trim_start_matches("drivesync: ").to_string())
                }
            });
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_folders_loading(false);
                match result {
                    Ok(json) => backend.as_mut().set_folders_json(QString::from(&json)),
                    Err(err) => backend.as_mut().set_folders_error(QString::from(&err)),
                }
            });
        });
    }

    fn save_folders(mut self: Pin<&mut Self>, names_json: &QString) {
        let names: Vec<String> = serde_json::from_str(&names_json.to_string()).unwrap_or_default();
        let names: Vec<String> = names.into_iter().filter(|n| !n.is_empty() && !n.contains('/')).collect();
        if let Err(err) = change_settings(|s| {
            s.insert("folders".into(), Value::from(names));
            s.insert("sync_requested".into(), Value::from(now_nanos()));
        }) {
            self.as_mut().set_folders_error(QString::from(&err));
        }
    }

    fn sign_in(mut self: Pin<&mut Self>, client_id: &QString, client_secret: &QString) {
        let id = client_id.to_string().trim().to_string();
        let secret = client_secret.to_string().trim().to_string();
        if id.is_empty() || secret.is_empty() || id.contains('\n') || secret.contains('\n') {
            self.as_mut().set_sign_in_log(QString::from("Paste both the client ID and the client secret."));
            return;
        }
        self.as_mut().set_signing_in(true);
        self.as_mut().set_sign_in_log(QString::from("Starting sign-in…"));
        let handle = self.qt_thread();
        let supervisor = Arc::clone(&self.rust().supervisor);
        std::thread::spawn(move || {
            let log_handle = handle.clone();
            let mut log = Vec::new();
            let result = engine::run_streaming(&["login", "--stdin"], &format!("{id}\n{secret}\n"), |line| {
                log.push(line.trim_start_matches("drivesync: ").to_string());
                let text = log.join("\n");
                let _ = log_handle.queue(move |mut backend| backend.as_mut().set_sign_in_log(QString::from(&text)));
            });
            let ok = matches!(result, Ok(true));
            if let Err(err) = &result {
                let text = err.clone();
                let _ = handle.queue(move |mut backend| backend.as_mut().set_sign_in_log(QString::from(&text)));
            }
            if ok {
                supervisor.restart();
            }
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_signing_in(false);
                if ok {
                    backend.as_mut().signed_in();
                }
            });
        });
    }

    fn sign_out(self: Pin<&mut Self>) {
        let supervisor = Arc::clone(&self.rust().supervisor);
        std::thread::spawn(move || {
            supervisor.hold();
            let _ = engine::run(&["logout"]);
            supervisor.restart();
        });
    }

    fn set_login_start(mut self: Pin<&mut Self>, enabled: bool) {
        match autostart::set(enabled).and_then(|()| {
            change_settings(|s| {
                s.insert("gui_start_at_login".into(), Value::Bool(enabled));
            })
        }) {
            Ok(()) => self.as_mut().set_start_at_login(enabled),
            Err(err) => {
                self.as_mut().set_start_at_login(!enabled);
                self.as_mut().alert(QString::from("Start at login"), QString::from(&err));
            }
        }
    }

    fn shutdown(self: Pin<&mut Self>) {
        self.rust().supervisor.stop();
    }
}
