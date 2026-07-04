pub mod backtest;
pub mod grpc;
pub mod metrics_server;
pub mod monitoring;
pub mod proto;
pub mod scoring;
pub mod screening;

pub const VERSION: &str = env!("CARGO_PKG_VERSION");

pub struct HealthStatus {
    pub status: String,
    pub version: String,
}

pub fn health() -> HealthStatus {
    HealthStatus {
        status: "ok".to_string(),
        version: VERSION.to_string(),
    }
}

pub fn version() -> &'static str {
    VERSION
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_health_returns_ok() {
        let status = health();
        assert_eq!(status.status, "ok");
        assert!(!status.version.is_empty());
    }

    #[test]
    fn test_version_not_empty() {
        assert!(!version().is_empty());
    }
}
