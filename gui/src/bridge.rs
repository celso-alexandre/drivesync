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
        #[qproperty(QString, accounts_json)]
        #[qproperty(bool, loaded)]
        #[qproperty(QString, engine_error)]
        #[qproperty(bool, start_at_login)]
        #[qproperty(QString, folders_account)]
        #[qproperty(QString, folders_json)]
        #[qproperty(bool, folders_loading)]
        #[qproperty(bool, folders_sizing)]
        #[qproperty(QString, folders_error)]
        #[qproperty(bool, signing_in)]
        #[qproperty(QString, sign_in_log)]
        #[qproperty(QString, sign_in_error)]
        type Backend = super::BackendRust;

        #[qsignal]
        fn alert(self: Pin<&mut Backend>, title: QString, body: QString);

        #[qsignal]
        fn signed_in(self: Pin<&mut Backend>, account: QString);

        #[qinvokable]
        fn start(self: Pin<&mut Backend>);

        #[qinvokable]
        fn sync_now(self: Pin<&mut Backend>, account: &QString);

        #[qinvokable]
        fn set_sync_paused(self: Pin<&mut Backend>, account: &QString, paused: bool);

        #[qinvokable]
        fn load_folders(self: Pin<&mut Backend>, account: &QString);

        #[qinvokable]
        fn save_folders(self: Pin<&mut Backend>, account: &QString, names_json: &QString);

        #[qinvokable]
        fn sign_in(self: Pin<&mut Backend>, client_id: &QString, client_secret: &QString);

        #[qinvokable]
        fn add_account(self: Pin<&mut Backend>);

        #[qinvokable]
        fn cancel_sign_in(self: Pin<&mut Backend>);

        #[qinvokable]
        fn remove_account(self: Pin<&mut Backend>, account: &QString);

        #[qinvokable]
        fn answer(self: Pin<&mut Backend>, account: &QString, id: &QString, choice: &QString);

        #[qinvokable]
        fn free_space(self: Pin<&mut Backend>, account: &QString) -> f64;

        #[qinvokable]
        fn set_login_start(self: Pin<&mut Backend>, enabled: bool);

        #[qinvokable]
        fn shutdown(self: Pin<&mut Backend>);
    }

    impl cxx_qt::Threading for Backend {}
}

use crate::accounts::{self, Account};
use crate::engine::{self, Paths, Supervisor};
use crate::{autostart, settings};
use core::pin::Pin;
use cxx_qt::{CxxQtType, Threading};
use cxx_qt_lib::QString;
use serde_json::{Value, json};
use std::collections::HashMap;
use std::path::PathBuf;
use std::process::Child;
use std::sync::{Arc, Mutex, PoisonError};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

struct Runtime {
    account: Account,
    paths: Paths,
    supervisor: Arc<Supervisor>,
}

#[derive(Default)]
struct Shared {
    registry: PathBuf,
    runtimes: Vec<Runtime>,
}

type SharedState = Arc<Mutex<Shared>>;

fn lock(shared: &SharedState) -> std::sync::MutexGuard<'_, Shared> {
    shared.lock().unwrap_or_else(PoisonError::into_inner)
}

pub struct BackendRust {
    accounts_json: QString,
    loaded: bool,
    engine_error: QString,
    start_at_login: bool,
    folders_account: QString,
    folders_json: QString,
    folders_loading: bool,
    folders_sizing: bool,
    folders_error: QString,
    signing_in: bool,
    sign_in_log: QString,
    sign_in_error: QString,
    shared: SharedState,
    login: Arc<Mutex<Option<Child>>>,
    started: bool,
}

impl Default for BackendRust {
    fn default() -> Self {
        Self {
            accounts_json: QString::from("[]"),
            loaded: false,
            engine_error: QString::default(),
            start_at_login: false,
            folders_account: QString::default(),
            folders_json: QString::from("[]"),
            folders_loading: false,
            folders_sizing: false,
            folders_error: QString::default(),
            signing_in: false,
            sign_in_log: QString::default(),
            sign_in_error: QString::default(),
            shared: Arc::new(Mutex::new(Shared::default())),
            login: Arc::new(Mutex::new(None)),
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

fn config_of(shared: &SharedState, account: &str) -> Option<PathBuf> {
    lock(shared).runtimes.iter().find(|r| r.account.id == account).map(|r| r.paths.config.clone())
}

fn change_account_settings(shared: &SharedState, account: &str, change: impl FnOnce(&mut serde_json::Map<String, Value>)) -> Result<(), String> {
    let config = config_of(shared, account).ok_or("unknown account")?;
    settings::update(&config, change)
}

fn start_runtime(shared: &SharedState, account: Account) -> Result<(), String> {
    let paths = engine::paths(&account.id)?;
    let supervisor = Supervisor::new(&account.id);
    supervisor.spawn_loop();
    lock(shared).runtimes.push(Runtime { account, paths, supervisor });
    Ok(())
}

#[derive(Default)]
struct Seen {
    status: Option<SystemTime>,
    settings: Option<SystemTime>,
    newest: Option<String>,
    snapshot: Value,
}

fn account_snapshot(runtime: &Runtime, seen: &mut Seen, alerts: &mut Vec<(String, String)>) -> bool {
    let settings_time = std::fs::metadata(&runtime.paths.config).and_then(|m| m.modified()).ok();
    let status_time = std::fs::metadata(&runtime.paths.status).and_then(|m| m.modified()).ok();
    if settings_time == seen.settings && status_time == seen.status && !seen.snapshot.is_null() {
        return false;
    }
    let current = settings::read(&runtime.paths.config);
    let status = engine::read_status(&runtime.paths).unwrap_or_default();
    if status.state == "signed_out" {
        runtime.supervisor.hold();
    }
    if status_time != seen.status {
        if let Some(newest) = &seen.newest {
            for event in status.recent.iter().take_while(|e| &e.time > newest) {
                if ALERT_KINDS.contains(&event.kind.as_str()) {
                    let place = if event.path.is_empty() { runtime.account.email.clone() } else { format!("{} · {}", runtime.account.email, event.path) };
                    alerts.push((alert_title(&event.kind).to_string(), format!("{place}: {}", event.detail)));
                }
            }
        }
        seen.newest = Some(status.recent.first().map(|e| e.time.clone()).unwrap_or_default());
    }
    seen.settings = settings_time;
    seen.status = status_time;
    let root = current.get("root").and_then(Value::as_str).map(str::to_string).unwrap_or_else(|| runtime.paths.root.display().to_string());
    let snapshot = json!({
        "id": runtime.account.id,
        "email": runtime.account.email,
        "root": root,
        "state": if status.state.is_empty() { "starting".to_string() } else { status.state.clone() },
        "message": status.message,
        "last_sync": status.last_sync,
        "progress": status.progress.unwrap_or(Value::Null),
        "attention": status.attention,
        "recent": status.recent.iter().take(20).map(|e| json!({"time": e.time, "kind": e.kind, "path": e.path, "detail": e.detail})).collect::<Vec<_>>(),
        "selected": current.get("folders").cloned().unwrap_or(Value::Array(Vec::new())),
        "paused": current.get("paused").and_then(Value::as_bool).unwrap_or(false),
    });
    if snapshot == seen.snapshot {
        return false;
    }
    seen.snapshot = snapshot;
    true
}

fn watch(handle: Handle, shared: SharedState) {
    let mut seen: HashMap<String, Seen> = HashMap::new();
    let mut last_ids: Vec<String> = Vec::new();
    loop {
        let mut alerts = Vec::new();
        let mut changed = false;
        let (ids, snapshots) = {
            let state = lock(&shared);
            let ids: Vec<String> = state.runtimes.iter().map(|r| r.account.id.clone()).collect();
            for runtime in &state.runtimes {
                let entry = seen.entry(runtime.account.id.clone()).or_default();
                changed |= account_snapshot(runtime, entry, &mut alerts);
            }
            let snapshots: Vec<Value> = state.runtimes.iter().map(|r| seen.get(&r.account.id).map(|s| s.snapshot.clone()).unwrap_or(Value::Null)).collect();
            (ids, snapshots)
        };
        seen.retain(|id, _| ids.contains(id));
        if ids != last_ids {
            changed = true;
            last_ids = ids;
        }
        if changed || !alerts.is_empty() {
            let text = Value::Array(snapshots).to_string();
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_accounts_json(QString::from(&text));
                backend.as_mut().set_loaded(true);
                for (title, body) in alerts {
                    backend.as_mut().alert(QString::from(&title), QString::from(&body));
                }
            });
        }
        std::thread::sleep(Duration::from_millis(700));
    }
}

fn finish_login(shared: &SharedState, registry: &std::path::Path, account: Account) -> Result<(), String> {
    let paths = engine::paths(&account.id)?;
    let root = accounts::root_for(&account.email).ok_or("home folder unknown")?;
    settings::update(&paths.config, |s| {
        s.insert("root".into(), Value::String(root.display().to_string()));
        s.insert("interval_seconds".into(), Value::from(30));
    })?;
    let mut all: Vec<Account> = lock(shared).runtimes.iter().map(|r| r.account.clone()).collect();
    all.push(account.clone());
    accounts::save(registry, &all)?;
    start_runtime(shared, account)
}

impl qobject::Backend {
    fn start(mut self: Pin<&mut Self>) {
        if self.rust().started {
            return;
        }
        self.as_mut().rust_mut().started = true;
        let handle = self.qt_thread();
        let shared = Arc::clone(&self.rust().shared);
        std::thread::spawn(move || {
            let base = match engine::paths("") {
                Ok(paths) => paths,
                Err(err) => {
                    let _ = handle.queue(move |mut backend| {
                        backend.as_mut().set_engine_error(QString::from(&err));
                        backend.as_mut().set_loaded(true);
                    });
                    return;
                }
            };
            let registry = base.config.parent().map(|dir| dir.join("accounts.json")).unwrap_or_default();
            let login = settings::read(&registry.with_file_name("gui.json")).get("start_at_login").and_then(Value::as_bool).unwrap_or(false);
            lock(&shared).registry = registry.clone();
            for account in accounts::load(&registry) {
                if let Err(err) = start_runtime(&shared, account) {
                    eprintln!("drivesync-gui: {err}");
                }
            }
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_start_at_login(login);
                backend.as_mut().set_loaded(true);
            });
            watch(handle, shared);
        });
    }

    fn configs_for(&self, account: &QString) -> Vec<PathBuf> {
        let wanted = account.to_string();
        lock(&self.rust().shared).runtimes.iter().filter(|r| wanted.is_empty() || r.account.id == wanted).map(|r| r.paths.config.clone()).collect()
    }

    fn sync_now(self: Pin<&mut Self>, account: &QString) {
        for config in self.configs_for(account) {
            if let Err(err) = settings::update(&config, |s| {
                s.insert("sync_requested".into(), Value::from(now_nanos()));
            }) {
                eprintln!("drivesync-gui: {err}");
            }
        }
    }

    fn set_sync_paused(mut self: Pin<&mut Self>, account: &QString, paused: bool) {
        for config in self.configs_for(account) {
            if let Err(err) = settings::update(&config, |s| {
                s.insert("paused".into(), Value::Bool(paused));
            }) {
                self.as_mut().alert(QString::from("drivesync"), QString::from(&err));
            }
        }
    }

    fn load_folders(mut self: Pin<&mut Self>, account: &QString) {
        let id = account.to_string();
        self.as_mut().set_folders_account(account.clone());
        self.as_mut().set_folders_json(QString::from("[]"));
        self.as_mut().set_folders_loading(true);
        self.as_mut().set_folders_sizing(false);
        self.as_mut().set_folders_error(QString::default());
        let handle = self.qt_thread();
        std::thread::spawn(move || {
            let list = |args: &[&str]| {
                engine::run(&engine::scoped(&id, args)).and_then(|out| if out.ok { Ok(out.stdout.trim().to_string()) } else { Err(engine::message(&out)) })
            };
            let quick = list(&["folders", "--json"]);
            let ok = quick.is_ok();
            let first = handle.clone();
            let _ = first.queue(move |mut backend| {
                backend.as_mut().set_folders_loading(false);
                match quick {
                    Ok(json) => {
                        backend.as_mut().set_folders_json(QString::from(&json));
                        backend.as_mut().set_folders_sizing(true);
                    }
                    Err(err) => backend.as_mut().set_folders_error(QString::from(&err)),
                }
            });
            if !ok {
                return;
            }
            let sized = list(&["folders", "--json", "--sizes"]);
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_folders_sizing(false);
                if let Ok(json) = sized {
                    backend.as_mut().set_folders_json(QString::from(&json));
                }
            });
        });
    }

    fn save_folders(mut self: Pin<&mut Self>, account: &QString, names_json: &QString) {
        let names: Vec<String> = serde_json::from_str(&names_json.to_string()).unwrap_or_default();
        let names: Vec<String> = names.into_iter().filter(|n| !n.is_empty() && !n.contains('/')).collect();
        let shared = Arc::clone(&self.rust().shared);
        if let Err(err) = change_account_settings(&shared, &account.to_string(), |s| {
            s.insert("folders".into(), Value::from(names));
            s.insert("sync_requested".into(), Value::from(now_nanos()));
        }) {
            self.as_mut().set_folders_error(QString::from(&err));
        }
    }

    fn sign_in(self: Pin<&mut Self>, client_id: &QString, client_secret: &QString) {
        let id = client_id.to_string().trim().to_string();
        let secret = client_secret.to_string().trim().to_string();
        if id.is_empty() || secret.is_empty() || id.contains('\n') || secret.contains('\n') {
            return;
        }
        self.begin_login(None, format!("{id}\n{secret}\n"));
    }

    fn add_account(self: Pin<&mut Self>) {
        let first = lock(&self.rust().shared).runtimes.first().map(|r| r.account.id.clone());
        if let Some(first) = first {
            self.begin_login(Some(first), String::new());
        }
    }

    fn begin_login(mut self: Pin<&mut Self>, reuse_from: Option<String>, input: String) {
        if self.rust().signing_in {
            return;
        }
        self.as_mut().set_signing_in(true);
        self.as_mut().set_sign_in_log(QString::default());
        self.as_mut().set_sign_in_error(QString::default());
        let handle = self.qt_thread();
        let shared = Arc::clone(&self.rust().shared);
        let slot = Arc::clone(&self.rust().login);
        std::thread::spawn(move || {
            let (registry, taken) = {
                let state = lock(&shared);
                (state.registry.clone(), state.runtimes.iter().map(|r| r.account.clone()).collect::<Vec<_>>())
            };
            let id = accounts::new_id(&taken);
            let mut args = vec!["login", "--stdin"];
            if let Some(from) = &reuse_from {
                args.push("--reuse-from");
                args.push(from);
            }
            let log_handle = handle.clone();
            let mut log = Vec::new();
            let mut email = String::new();
            let result = engine::run_streaming(&engine::scoped(&id, &args), &input, &slot, |line| {
                if let Some(found) = line.strip_prefix("ACCOUNT_EMAIL ") {
                    email = found.trim().to_string();
                    return;
                }
                log.push(line.trim_start_matches("drivesync: ").to_string());
                let text = log.join("\n");
                let _ = log_handle.queue(move |mut backend| backend.as_mut().set_sign_in_log(QString::from(&text)));
            });
            let outcome = match result {
                Ok(true) if email.is_empty() => Err("Google did not say which account signed in.".to_string()),
                Ok(true) if taken.iter().any(|a| a.email.eq_ignore_ascii_case(&email)) => {
                    let _ = engine::run(&engine::scoped(&id, &["logout"]));
                    Err(format!("{email} is already in drivesync."))
                }
                Ok(true) => finish_login(&shared, &registry, Account { id: id.clone(), email: email.clone() }),
                Ok(false) => Err(String::new()),
                Err(err) => Err(err),
            };
            let _ = handle.queue(move |mut backend| {
                backend.as_mut().set_signing_in(false);
                match outcome {
                    Ok(()) => backend.as_mut().signed_in(QString::from(&id)),
                    Err(err) => backend.as_mut().set_sign_in_error(QString::from(&err)),
                }
            });
        });
    }

    fn cancel_sign_in(self: Pin<&mut Self>) {
        if let Some(child) = self.rust().login.lock().unwrap_or_else(PoisonError::into_inner).as_mut() {
            let _ = child.kill();
        }
    }

    fn remove_account(self: Pin<&mut Self>, account: &QString) {
        let id = account.to_string();
        let shared = Arc::clone(&self.rust().shared);
        let handle = self.qt_thread();
        std::thread::spawn(move || {
            let removed = {
                let mut state = lock(&shared);
                let index = state.runtimes.iter().position(|r| r.account.id == id);
                index.map(|i| state.runtimes.remove(i))
            };
            let Some(runtime) = removed else { return };
            runtime.supervisor.stop();
            let _ = engine::run(&engine::scoped(&id, &["logout"]));
            let (registry, remaining) = {
                let state = lock(&shared);
                (state.registry.clone(), state.runtimes.iter().map(|r| r.account.clone()).collect::<Vec<_>>())
            };
            let result = accounts::save(&registry, &remaining);
            for dir in [accounts::owned_dir(&runtime.paths.config, &id), accounts::owned_dir(&runtime.paths.status, &id)].into_iter().flatten() {
                let _ = std::fs::remove_dir_all(dir);
            }
            if let Err(err) = result {
                let _ = handle.queue(move |mut backend| backend.as_mut().alert(QString::from("drivesync"), QString::from(&err)));
            }
        });
    }

    fn answer(mut self: Pin<&mut Self>, account: &QString, id: &QString, choice: &QString) {
        let id = id.to_string();
        let choice = choice.to_string();
        if choice != "keep" && choice != "delete" {
            return;
        }
        let shared = Arc::clone(&self.rust().shared);
        if let Err(err) = change_account_settings(&shared, &account.to_string(), |s| {
            let mut decisions = s.get("decisions").and_then(Value::as_object).cloned().unwrap_or_default();
            decisions.insert(id, Value::String(choice));
            s.insert("decisions".into(), Value::Object(decisions));
            s.insert("sync_requested".into(), Value::from(now_nanos()));
        }) {
            self.as_mut().alert(QString::from("drivesync"), QString::from(&err));
        }
    }

    fn free_space(self: Pin<&mut Self>, account: &QString) -> f64 {
        let wanted = account.to_string();
        let root = {
            let state = lock(&self.rust().shared);
            state.runtimes.iter().find(|r| r.account.id == wanted).map(|r| {
                settings::read(&r.paths.config).get("root").and_then(Value::as_str).map(PathBuf::from).unwrap_or_else(|| r.paths.root.clone())
            })
        };
        let mut path = root.or_else(accounts::home).unwrap_or_default();
        while !path.as_os_str().is_empty() && !path.exists() {
            if !path.pop() {
                break;
            }
        }
        crate::entry::ffi::free_bytes(&path.display().to_string()) as f64
    }

    fn set_login_start(mut self: Pin<&mut Self>, enabled: bool) {
        let registry = lock(&self.rust().shared).registry.clone();
        match autostart::set(enabled).and_then(|()| {
            settings::update(&registry.with_file_name("gui.json"), |s| {
                s.insert("start_at_login".into(), Value::Bool(enabled));
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
        for runtime in &lock(&self.rust().shared).runtimes {
            runtime.supervisor.stop();
        }
        if let Some(child) = self.rust().login.lock().unwrap_or_else(PoisonError::into_inner).as_mut() {
            let _ = child.kill();
        }
    }
}
