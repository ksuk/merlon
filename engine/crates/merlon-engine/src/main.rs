use merlon_engine::grpc::scoring_service::ScoringServiceImpl;
use merlon_engine::proto::merlon::v1::scoring_service_server::ScoringServiceServer;
use merlon_engine::scoring::config::CddWeightConfig;
use merlon_engine::scoring::engine::CddScoringEngine;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let config_path = std::env::var("MERLON_CDD_WEIGHTS_PATH")
        .unwrap_or_else(|_| "cdd_weights.yaml".to_string());

    let config = CddWeightConfig::load(&config_path)?;
    let engine = CddScoringEngine::new(config)?;
    let service = ScoringServiceImpl::new(engine);

    let addr = std::env::var("MERLON_ENGINE_ADDR")
        .unwrap_or_else(|_| "[::]:50051".to_string())
        .parse()?;

    eprintln!(
        "merlon-engine v{} starting gRPC server on {}",
        merlon_engine::VERSION,
        addr
    );

    Server::builder()
        .add_service(ScoringServiceServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
