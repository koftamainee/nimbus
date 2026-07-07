use serde::Deserialize;
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Deserialize, PartialEq)]
pub struct Manifest {
    #[serde(default)]
    pub entrypoint: Vec<String>,
    #[serde(default)]
    pub cmd: Vec<String>,
    #[serde(default = "default_env")]
    pub env: Vec<String>,
    #[serde(default = "default_workdir")]
    pub workdir: String,
}

fn default_env() -> Vec<String> {
    vec!["PATH=/usr/bin:/bin:/usr/sbin:/sbin".to_string()]
}

fn default_workdir() -> String {
    "/".to_string()
}

pub fn unpack_image(tar_path: &str, dest: &Path) -> anyhow::Result<Manifest> {
    let file = fs::File::open(tar_path)
        .map_err(|e| anyhow::anyhow!("cannot open image '{}': {}", tar_path, e))?;

    if tar_path.ends_with(".tar.gz") || tar_path.ends_with(".tgz") {
        let decoder = flate2::read::GzDecoder::new(file);
        let mut archive = tar::Archive::new(decoder);
        archive.unpack(dest)?;
    } else {
        let mut archive = tar::Archive::new(file);
        archive.unpack(dest)?;
    }

    let manifest_path = dest.join("manifest.toml");
    if manifest_path.exists() {
        let data = fs::read_to_string(&manifest_path)?;
        let manifest: Manifest = toml::from_str(&data)?;
        Ok(manifest)
    } else {
        Ok(Manifest {
            entrypoint: vec![],
            cmd: vec![],
            env: default_env(),
            workdir: default_workdir(),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_manifest_defaults() {
        let m: Manifest = toml::from_str("").unwrap();
        assert!(m.entrypoint.is_empty());
        assert!(m.cmd.is_empty());
        assert_eq!(m.env, vec!["PATH=/usr/bin:/bin:/usr/sbin:/sbin"]);
        assert_eq!(m.workdir, "/");
    }

    #[test]
    fn test_manifest_full() {
        let toml_str = r#"
entrypoint = ["/app/server"]
cmd = ["--port", "8080"]
env = ["PATH=/usr/bin", "HOME=/root"]
workdir = "/app"
"#;
        let m: Manifest = toml::from_str(toml_str).unwrap();
        assert_eq!(m.entrypoint, vec!["/app/server"]);
        assert_eq!(m.cmd, vec!["--port", "8080"]);
        assert_eq!(m.env, vec!["PATH=/usr/bin", "HOME=/root"]);
        assert_eq!(m.workdir, "/app");
    }

    #[test]
    fn test_manifest_partial() {
        let toml_str = r#"entrypoint = ["/bin/bash"]"#;
        let m: Manifest = toml::from_str(toml_str).unwrap();
        assert_eq!(m.entrypoint, vec!["/bin/bash"]);
        assert!(m.cmd.is_empty());
        assert!(!m.env.is_empty());
        assert_eq!(m.workdir, "/");
    }

    #[test]
    fn test_manifest_cmd_only() {
        let toml_str = r#"cmd = ["echo", "hello"]"#;
        let m: Manifest = toml::from_str(toml_str).unwrap();
        assert_eq!(m.cmd, vec!["echo", "hello"]);
        assert!(m.entrypoint.is_empty());
    }

    #[test]
    fn test_manifest_env_inherits_default() {
        let m: Manifest = toml::from_str("workdir = '/opt'").unwrap();
        assert_eq!(m.env, vec!["PATH=/usr/bin:/bin:/usr/sbin:/sbin"]);
    }

    #[test]
    fn test_unpack_image_nonexistent() {
        let result = unpack_image("/nonexistent/image.tar", Path::new("/tmp"));
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("cannot open image"));
    }
}
