use std::sync::Arc;

use merlon_engine::grpc::backtest_service::BacktestServiceImpl;
use merlon_engine::grpc::config_service::ConfigServiceImpl;
use merlon_engine::grpc::monitoring_service::MonitoringServiceImpl;
use merlon_engine::grpc::scoring_service::ScoringServiceImpl;
use merlon_engine::grpc::screening_service::ScreeningServiceImpl;
use merlon_engine::monitoring::config::ScenarioConfig;
use merlon_engine::monitoring::engine::TmEngine;
use merlon_engine::proto::merlon::v1::backtest_service_server::BacktestServiceServer;
use merlon_engine::proto::merlon::v1::config_service_server::ConfigServiceServer;
use merlon_engine::proto::merlon::v1::monitoring_service_server::MonitoringServiceServer;
use merlon_engine::proto::merlon::v1::scoring_service_server::ScoringServiceServer;
use merlon_engine::proto::merlon::v1::screening_service_server::ScreeningServiceServer;
use merlon_engine::scoring::config::CddWeightConfig;
use merlon_engine::scoring::engine::CddScoringEngine;
use merlon_engine::screening::config::ScreeningListConfig;
use merlon_engine::screening::engine::ScreeningEngine;
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

    let tm_configs = load_yaml_configs::<ScenarioConfig>(&tm_paths)?;
    let tm_engine = Arc::new(TmEngine::new(tm_configs)?);
    let monitoring_service = MonitoringServiceImpl::from_arc(Arc::clone(&tm_engine));
    let backtest_service = BacktestServiceImpl::new(Arc::clone(&tm_engine));

    let screening_paths = std::env::var("MERLON_SCREENING_LISTS_PATH")
        .unwrap_or_else(|_| "screening_lists".to_string());

    let screening_threshold: f64 = std::env::var("MERLON_SCREENING_THRESHOLD")
        .unwrap_or_else(|_| "0.85".to_string())
        .parse()
        .unwrap_or(0.85);

    let screening_lists = load_yaml_configs::<ScreeningListConfig>(&screening_paths)?;
    let screening_engine = ScreeningEngine::new(screening_lists, screening_threshold)?;
    let screening_service = ScreeningServiceImpl::new(screening_engine);
    let config_service = ConfigServiceImpl::new();

    let addr = std::env::var("MERLON_ENGINE_ADDR")
        .unwrap_or_else(|_| "[::]:50051".to_string())
        .parse()?;

    eprintln!(
        "merlon-engine v{} starting gRPC server on {}",
        merlon_engine::VERSION,
        addr
    );

    Server::builder()
        .max_frame_size(Some(4 * 1024 * 1024))
        .add_service(ScoringServiceServer::new(scoring_service))
        .add_service(MonitoringServiceServer::new(monitoring_service))
        .add_service(ScreeningServiceServer::new(screening_service))
        .add_service(BacktestServiceServer::new(backtest_service))
        .add_service(ConfigServiceServer::new(config_service))
        .serve(addr)
        .await?;

    Ok(())
}

fn load_yaml_configs<T: serde::de::DeserializeOwned>(
    dir: &str,
) -> Result<Vec<T>, Box<dyn std::error::Error>> {
    let mut configs = Vec::new();
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().is_some_and(|ext| ext == "yaml" || ext == "yml") {
            let content = std::fs::read_to_string(&path)?;
            let config: T = serde_yaml::from_str(&content)?;
            configs.push(config);
        }
    }
    Ok(configs)
}
