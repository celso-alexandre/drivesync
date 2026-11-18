use serde_json::{Map, Value};
use std::path::Path;

pub fn read(path: &Path) -> Map<String, Value> {
    std::fs::read(path)
        .ok()
        .and_then(|data| serde_json::from_slice::<Value>(&data).ok())
        .and_then(|value| match value {
            Value::Object(map) => Some(map),
            _ => None,
        })
        .unwrap_or_default()
}

pub fn update(path: &Path, change: impl FnOnce(&mut Map<String, Value>)) -> Result<(), String> {
    let mut settings = read(path);
    change(&mut settings);
    let dir = path.parent().ok_or("settings path has no folder")?;
    std::fs::create_dir_all(dir).map_err(|err| err.to_string())?;
    let mut data = serde_json::to_vec_pretty(&Value::Object(settings)).map_err(|err| err.to_string())?;
    data.push(b'\n');
    let tmp = path.with_extension("json.tmp");
    write_private(&tmp, &data)?;
    std::fs::rename(&tmp, path).map_err(|err| err.to_string())
}

#[cfg(unix)]
fn write_private(path: &Path, data: &[u8]) -> Result<(), String> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(path)
        .map_err(|err| err.to_string())?;
    file.write_all(data).map_err(|err| err.to_string())
}

#[cfg(not(unix))]
fn write_private(path: &Path, data: &[u8]) -> Result<(), String> {
    std::fs::write(path, data).map_err(|err| err.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn update_keeps_unknown_keys_and_writes_atomically() {
        let dir = std::env::temp_dir().join(format!("drivesync-gui-test-{}", std::process::id()));
        let path = dir.join("config.json");
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(&path, r#"{"root":"/x","interval_seconds":30,"future":true}"#).unwrap();
        update(&path, |m| {
            m.insert("paused".into(), Value::Bool(true));
        })
        .unwrap();
        let settings = read(&path);
        assert_eq!(settings["root"], "/x");
        assert_eq!(settings["future"], true);
        assert_eq!(settings["paused"], true);
        assert!(!path.with_extension("json.tmp").exists());
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
