fn main() {
    let status = merlon_engine::health();
    println!(
        "merlon-engine v{} status={}",
        merlon_engine::VERSION,
        status.status
    );
}
