use cxx_qt_build::{CxxQtBuilder, QmlModule};

fn main() {
    let linux = std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("linux");
    let mut builder = CxxQtBuilder::new_qml_module(QmlModule::new("drivesync").qml_files(["qml/Main.qml"]))
        .file("src/bridge.rs")
        .file("src/entry.rs")
        .qt_module("Widgets")
        .qt_module("Network")
        .cpp_file("cpp/run.cpp")
        .include_prefix("drivesync-gui")
        .qrc_resources([
            "icons/drivesync.svg",
            "icons/tray-idle.svg",
            "icons/tray-syncing.svg",
            "icons/tray-paused.svg",
            "icons/tray-error.svg",
            "icons/tray-offline.svg",
        ]);
    if linux {
        builder = builder.qt_module("DBus");
    }
    builder.build();
    println!("cargo:rustc-link-arg=-lQt6Widgets");
    println!("cargo:rustc-link-arg=-lQt6Network");
    if linux {
        println!("cargo:rustc-link-arg=-lQt6DBus");
        println!("cargo:rustc-cfg=portal");
    }
    println!("cargo::rustc-check-cfg=cfg(portal)");
}
