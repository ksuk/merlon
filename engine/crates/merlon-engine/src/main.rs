use std::sync::Arc;

use merlon_engine::grpc::backtest_service::BacktestServiceImpl;
use merlon_engine::grpc::config_service::ConfigServiceImpl;
use merlon_engine::grpc::metrics_interceptor::MetricsLayer;
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

    let tm_configs = load_tm_scenario_configs(&tm_paths)?;
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

    // Standard grpc.health.v1 health check (OPS-002, overview.md §4.4),
    // superseding the per-service deprecated custom Health rpc. The overall
    // ("" service name) check is SERVING as soon as health_reporter() is
    // created; this process has nothing left to initialize asynchronously
    // after the services above are constructed. Each individual service is
    // also registered so `grpc_health_probe -service=<name>` works.
    let (health_reporter, health_service) = tonic_health::server::health_reporter();
    health_reporter
        .set_serving::<ScoringServiceServer<ScoringServiceImpl>>()
        .await;
    health_reporter
        .set_serving::<MonitoringServiceServer<MonitoringServiceImpl>>()
        .await;
    health_reporter
        .set_serving::<ScreeningServiceServer<ScreeningServiceImpl>>()
        .await;
    health_reporter
        .set_serving::<BacktestServiceServer<BacktestServiceImpl>>()
        .await;
    health_reporter
        .set_serving::<ConfigServiceServer<ConfigServiceImpl>>()
        .await;

    eprintln!(
        "merlon-engine v{} starting gRPC server on {}",
        merlon_engine::VERSION,
        addr
    );

    Server::builder()
        .max_frame_size(Some(4 * 1024 * 1024))
        .layer(MetricsLayer)
        .add_service(health_service)
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

/// Loads TM scenario content with the v1/v2 dual loader (rule-schema.md
/// §3.1), unlike `load_yaml_configs` which the CDD weight/screening list
/// configs still use (those have no v2 format yet).
fn load_tm_scenario_configs(dir: &str) -> Result<Vec<ScenarioConfig>, Box<dyn std::error::Error>> {
    let mut configs = Vec::new();
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().is_some_and(|ext| ext == "yaml" || ext == "yml") {
            let content = std::fs::read_to_string(&path)?;
            configs.push(ScenarioConfig::from_yaml_dual(&content)?);
        }
    }
    Ok(configs)
}
