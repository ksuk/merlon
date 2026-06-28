use merlon_engine::grpc::monitoring_service::MonitoringServiceImpl;
use merlon_engine::grpc::scoring_service::ScoringServiceImpl;
use merlon_engine::monitoring::config::ScenarioConfig;
use merlon_engine::monitoring::engine::TmEngine;
use merlon_engine::proto::merlon::v1::monitoring_service_server::MonitoringServiceServer;
use merlon_engine::proto::merlon::v1::scoring_service_server::ScoringServiceServer;
use merlon_engine::scoring::config::CddWeightConfig;
use merlon_engine::scoring::engine::CddScoringEngine;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let cdd_path = std::env::var("MERLON_CDD_WEIGHTS_PATH")
        .unwrap_or_else(|_| "cdd_weights.yaml".to_string());

    let cdd_config = CddWeightConfig::load(&cdd_path)?;
    let cdd_engine = CddScoringEngine::new(cdd_config)?;
    let scoring_service = ScoringServiceImpl::new(cdd_engine);

    let tm_paths = std::env::var("MERLON_TM_SCENARIOS_PATH")
        .unwrap_or_else(|_| "tm_scenarios".to_string());

    let tm_configs = load_scenario_configs(&tm_paths)?;
    let tm_engine = TmEngine::new(tm_configs)?;
    let monitoring_service = MonitoringServiceImpl::new(tm_engine);

    let addr = std::env::var("MERLON_ENGINE_ADDR")
        .unwrap_or_else(|_| "[::]:50051".to_string())
        .parse()?;

    eprintln!(
        "merlon-engine v{} starting gRPC server on {}",
        merlon_engine::VERSION,
        addr
    );

    Server::builder()
        .add_service(ScoringServiceServer::new(scoring_service))
        .add_service(MonitoringServiceServer::new(monitoring_service))
        .serve(addr)
        .await?;

    Ok(())
}

fn load_scenario_configs(
    dir: &str,
) -> Result<Vec<ScenarioConfig>, Box<dyn std::error::Error>> {
    let mut configs = Vec::new();
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().is_some_and(|ext| ext == "yaml" || ext == "yml") {
            let config = ScenarioConfig::load(path.to_str().unwrap_or_default())?;
            configs.push(config);
        }
    }
    Ok(configs)
}
