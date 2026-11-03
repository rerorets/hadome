//! hadome is a Go binary. This crate only lets Rust projects pin it with `cargo binstall`, which
//! downloads the prebuilt binary of the release. Building the crate from source gives this
//! program, which explains that.

fn main() {
    eprintln!(
        "hadome is distributed as a prebuilt binary. Install it with:\n\n    cargo binstall hadome@{}\n\n\
         (cargo install builds this placeholder instead.) See https://github.com/rerorets/hadome",
        env!("CARGO_PKG_VERSION")
    );
    std::process::exit(2);
}
