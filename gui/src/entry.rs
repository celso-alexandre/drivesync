#[cxx_qt::bridge]
pub mod ffi {
    unsafe extern "C++" {
        include!("drivesync-gui/cpp/run.h");
        fn run_app(start_hidden: bool) -> i32;
        fn request_background(autostart: bool) -> bool;
        fn free_bytes(path: &str) -> i64;
    }
}
