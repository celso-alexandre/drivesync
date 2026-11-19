#![deny(unsafe_code)]
#![cfg_attr(not(test), deny(clippy::unwrap_used, clippy::expect_used, clippy::panic))]

mod accounts;
mod autostart;
#[allow(unsafe_code)]
mod bridge;
mod engine;
#[allow(unsafe_code)]
mod entry;
mod settings;

fn main() {
    let start_hidden = std::env::args().skip(1).any(|arg| arg == "--hidden");
    std::process::exit(entry::ffi::run_app(start_hidden));
}
