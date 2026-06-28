fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_dir = "../../../proto";
    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(
            &[
                &format!("{proto_dir}/merlon/v1/scoring.proto"),
                &format!("{proto_dir}/merlon/v1/types.proto"),
                &format!("{proto_dir}/merlon/v1/monitoring.proto"),
            ],
            &[proto_dir],
        )?;
    Ok(())
}
