use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum ContainerStatus {
    Created,
    Running,
    Exited,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ContainerConfig {
    pub name: String,
    pub image: String,
    pub cmd: Vec<String>,
    pub status: ContainerStatus,
    pub pid: Option<u32>,
    pub created_at: String,
    pub started_at: Option<String>,
    pub finished_at: Option<String>,
    pub exit_code: Option<i32>,
    pub cgroup_path: String,
    pub memory_mb: Option<u64>,
    pub cpus: Option<f64>,
    pub env: Vec<String>,
}

impl ContainerConfig {
    pub fn path(data_dir: &Path, name: &str) -> std::path::PathBuf {
        data_dir.join("containers").join(name).join("config.json")
    }

    pub fn load(data_dir: &Path, name: &str) -> anyhow::Result<Self> {
        let path = Self::path(data_dir, name);
        let data = std::fs::read_to_string(&path)
            .map_err(|e| anyhow::anyhow!("failed to read config for {}: {}", name, e))?;
        Ok(serde_json::from_str(&data)?)
    }

    pub fn save(&self, data_dir: &Path) -> anyhow::Result<()> {
        let path = Self::path(data_dir, &self.name);
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let data = serde_json::to_string_pretty(self)?;
        std::fs::write(&path, data.as_bytes())?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_config() -> ContainerConfig {
        ContainerConfig {
            name: "my-container".to_string(),
            image: "/images/test.tar".to_string(),
            cmd: vec!["/bin/sh".to_string()],
            status: ContainerStatus::Running,
            pid: Some(42),
            created_at: "2026-01-01T00:00:00Z".to_string(),
            started_at: Some("2026-01-01T00:00:01Z".to_string()),
            finished_at: None,
            exit_code: None,
            cgroup_path: "/sys/fs/cgroup/forge/test-01".to_string(),
            memory_mb: Some(512),
            cpus: Some(1.5),
            env: vec!["PATH=/usr/bin".to_string()],
        }
    }

    #[test]
    fn test_config_serialize_roundtrip() {
        let c = test_config();
        let json = serde_json::to_string_pretty(&c).unwrap();
        let c2: ContainerConfig = serde_json::from_str(&json).unwrap();
        assert_eq!(c, c2);
    }

    #[test]
    fn test_config_path() {
        let p = ContainerConfig::path(Path::new("/data"), "abc123");
        assert_eq!(p, Path::new("/data/containers/abc123/config.json"));
    }

    #[test]
    fn test_config_default_exited() {
        let c = ContainerConfig {
            status: ContainerStatus::Exited,
            exit_code: Some(0),
            finished_at: Some("2026-01-01T00:00:05Z".to_string()),
            ..test_config()
        };
        let json = serde_json::to_string_pretty(&c).unwrap();
        assert!(json.contains("\"exited\""));
    }

    #[test]
    fn test_config_status_variants() {
        let variants = vec![
            ContainerStatus::Created,
            ContainerStatus::Running,
            ContainerStatus::Exited,
        ];
        for v in &variants {
            let json = serde_json::to_string(v).unwrap();
            let back: ContainerStatus = serde_json::from_str(&json).unwrap();
            assert_eq!(*v, back);
        }
    }

    #[test]
    fn test_config_save_and_load() {
        let dir = tempfile::TempDir::new().unwrap();
        let c = test_config();
        let name = "save-load-test";
        let c = ContainerConfig {
            name: name.to_string(),
            ..c
        };
        c.save(dir.path()).unwrap();
        let loaded = ContainerConfig::load(dir.path(), name).unwrap();
        assert_eq!(c, loaded);
    }

    #[test]
    fn test_config_load_nonexistent() {
        let dir = tempfile::TempDir::new().unwrap();
        let result = ContainerConfig::load(dir.path(), "nonexistent");
        assert!(result.is_err());
    }
}
