fn main() {
    let mut res = winresource::WindowsResource::new();
    res.set_icon("picker.ico");
    res.compile().unwrap();
}
