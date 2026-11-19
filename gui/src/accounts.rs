use serde::{Deserialize, Serialize};
use std::collections::hash_map::RandomState;
use std::hash::{BuildHasher, Hasher};
use std::path::{Path, PathBuf};

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct Account {
    pub id: String,
    pub email: String,
}

pub fn load(path: &Path) -> Vec<Account> {
    std::fs::read(path).ok().and_then(|data| serde_json::from_slice(&data).ok()).unwrap_or_default()
}

pub fn save(path: &Path, accounts: &[Account]) -> Result<(), String> {
    let dir = path.parent().ok_or("accounts path has no folder")?;
    std::fs::create_dir_all(dir).map_err(|err| err.to_string())?;
    let mut data = serde_json::to_vec_pretty(accounts).map_err(|err| err.to_string())?;
    data.push(b'\n');
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, data).map_err(|err| err.to_string())?;
    std::fs::rename(&tmp, path).map_err(|err| err.to_string())
}

pub fn new_id(taken: &[Account]) -> String {
    loop {
        let mut hasher = RandomState::new().build_hasher();
        hasher.write_u128(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or_default());
        hasher.write_u32(std::process::id());
        let id = format!("{:012x}", hasher.finish() & 0xffff_ffff_ffff);
        if !taken.iter().any(|account| account.id == id) {
            return id;
        }
    }
}

pub fn folder_name(email: &str) -> String {
    let cleaned: String = email
        .chars()
        .map(|c| if c.is_control() || "<>:\"/\\|?*".contains(c) { '_' } else { c })
        .collect();
    let trimmed = cleaned.trim_matches(|c: char| c == '.' || c == ' ');
    if trimmed.is_empty() { "account".to_string() } else { trimmed.to_string() }
}

pub fn home() -> Option<PathBuf> {
    std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).map(PathBuf::from)
}

pub fn root_for(email: &str) -> Option<PathBuf> {
    home().map(|h| h.join("GoogleDrive").join(folder_name(email)))
}

pub fn owned_dir(path: &Path, id: &str) -> Option<PathBuf> {
    let dir = path.parent()?;
    let is_account_dir = dir.file_name()? == id && dir.parent()?.file_name()? == "accounts";
    is_account_dir.then(|| dir.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ids_are_valid_and_unique() {
        let mut taken = Vec::new();
        for _ in 0..200 {
            let id = new_id(&taken);
            assert!(id.len() == 12 && id.chars().all(|c| c.is_ascii_hexdigit() && !c.is_ascii_uppercase()));
            assert!(!taken.iter().any(|a: &Account| a.id == id));
            taken.push(Account { id, email: String::new() });
        }
    }

    #[test]
    fn folder_names_are_safe() {
        assert_eq!(folder_name("celso@gmail.com"), "celso@gmail.com");
        assert_eq!(folder_name("a/b\\c:d"), "a_b_c_d");
        assert_eq!(folder_name("../.."), "_");
        assert_eq!(folder_name(""), "account");
    }

    #[test]
    fn owned_dir_only_accepts_the_accounts_own_folder() {
        let id = "abc123";
        assert_eq!(owned_dir(Path::new("/c/drivesync/accounts/abc123/config.json"), id), Some(PathBuf::from("/c/drivesync/accounts/abc123")));
        assert_eq!(owned_dir(Path::new("/c/drivesync/config.json"), id), None);
        assert_eq!(owned_dir(Path::new("/c/drivesync/accounts/other/config.json"), id), None);
        assert_eq!(owned_dir(Path::new("/home/x/config.json"), id), None);
    }

    #[test]
    fn registry_round_trips() {
        let path = std::env::temp_dir().join(format!("drivesync-accounts-{}.json", std::process::id()));
        let accounts = vec![Account { id: "a".into(), email: "a@x".into() }, Account { id: "b".into(), email: "b@x".into() }];
        save(&path, &accounts).unwrap();
        assert_eq!(load(&path), accounts);
        std::fs::remove_file(&path).unwrap();
        assert!(load(&path).is_empty());
    }
}
