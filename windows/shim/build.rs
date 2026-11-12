// Windows records a hook procedure as an offset into its DLL and calls that offset
// in a process that already holds the DLL by the same path, whichever build it
// holds. The shim keeps one path across releases, so HookProc goes first in the
// code section in every build; the injector refuses a shim where it isn't.
fn main() {
    let order = std::path::Path::new(&std::env::var("CARGO_MANIFEST_DIR").unwrap()).join("order.txt");
    println!("cargo:rerun-if-changed={}", order.display());
    println!("cargo:rustc-cdylib-link-arg=/ORDER:@{}", order.display());
}
