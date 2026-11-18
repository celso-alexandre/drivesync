use std::path::PathBuf;

const APP_ID: &str = "io.github.celso_alexandre.drivesync";

pub fn set(enabled: bool) -> Result<(), String> {
    if std::env::var_os("FLATPAK_ID").is_some() {
        return flatpak(enabled);
    }
    native(enabled)
}

#[cfg(portal)]
fn flatpak(enabled: bool) -> Result<(), String> {
    if crate::entry::ffi::request_background(enabled) {
        Ok(())
    } else {
        Err("the desktop refused the background request".into())
    }
}

#[cfg(not(portal))]
fn flatpak(_enabled: bool) -> Result<(), String> {
    Err("start at login is not available here".into())
}

fn executable() -> Result<String, String> {
    std::env::current_exe().map(|path| path.display().to_string()).map_err(|err| err.to_string())
}

fn home() -> Result<PathBuf, String> {
    std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" })
        .map(PathBuf::from)
        .ok_or_else(|| "home folder unknown".to_string())
}

fn write_or_remove(path: PathBuf, content: Option<String>) -> Result<(), String> {
    match content {
        Some(text) => {
            if let Some(dir) = path.parent() {
                std::fs::create_dir_all(dir).map_err(|err| err.to_string())?;
            }
            std::fs::write(&path, text).map_err(|err| err.to_string())
        }
        None => match std::fs::remove_file(&path) {
            Err(err) if err.kind() != std::io::ErrorKind::NotFound => Err(err.to_string()),
            _ => Ok(()),
        },
    }
}

#[cfg(target_os = "linux")]
fn native(enabled: bool) -> Result<(), String> {
    let config = std::env::var_os("XDG_CONFIG_HOME").map(PathBuf::from).map_or_else(|| home().map(|h| h.join(".config")), Ok)?;
    let entry = format!(
        "[Desktop Entry]\nType=Application\nName=drivesync\nExec=\"{}\" --hidden\nIcon={APP_ID}\nX-GNOME-Autostart-enabled=true\n",
        executable()?
    );
    write_or_remove(config.join("autostart").join(format!("{APP_ID}.desktop")), enabled.then_some(entry))
}

#[cfg(target_os = "macos")]
fn native(enabled: bool) -> Result<(), String> {
    let plist = format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>{APP_ID}</string><key>ProgramArguments</key><array><string>{}</string><string>--hidden</string></array><key>RunAtLoad</key><true/><key>ProcessType</key><string>Interactive</string></dict></plist>\n",
        executable()?
    );
    write_or_remove(home()?.join("Library/LaunchAgents").join(format!("{APP_ID}.plist")), enabled.then_some(plist))
}

#[cfg(windows)]
fn native(enabled: bool) -> Result<(), String> {
    let key = r"HKCU\Software\Microsoft\Windows\CurrentVersion\Run";
    let mut command = std::process::Command::new("reg");
    if enabled {
        command.args(["add", key, "/v", "drivesync", "/t", "REG_SZ", "/d", &format!("\"{}\" --hidden", executable()?), "/f"]);
    } else {
        command.args(["delete", key, "/v", "drivesync", "/f"]);
    }
    let status = command.status().map_err(|err| err.to_string())?;
    if status.success() || !enabled {
        Ok(())
    } else {
        Err("could not register drivesync to start at sign-in".into())
    }
}
