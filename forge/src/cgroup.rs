use std::fs;
use std::path::Path;

pub struct Cgroup {
    path: std::path::PathBuf,
}

impl Cgroup {
    pub fn create(_data_dir: &Path, id: &str, memory_mb: Option<u64>, cpus: Option<f64>) -> anyhow::Result<Self> {
        let base = Path::new("/sys/fs/cgroup/forge");
        let path = base.join(id);

        if path.exists() {
            anyhow::bail!("cgroup {} already exists", id);
        }

        fs::create_dir_all(&path)?;
        write_file(&path.join("cgroup.type"), "threaded")?;

        if let Some(mb) = memory_mb {
            let max = format!("{}", mb * 1024 * 1024);
            let high = format!("{}", ((mb as f64) * 0.9 * 1024.0 * 1024.0) as u64);
            write_file(&path.join("memory.max"), &max)?;
            write_file(&path.join("memory.high"), &high)?;
        }

        if let Some(c) = cpus {
            let quota = (c * 100_000.0) as u64;
            let max = format!("{} 100000", quota);
            write_file(&path.join("cpu.max"), &max)?;
        }

        Ok(Cgroup { path })
    }

    pub fn add_pid(&self, pid: u32) -> anyhow::Result<()> {
        write_file(&self.path.join("cgroup.procs"), &pid.to_string())
    }

    pub fn remove(&self) -> anyhow::Result<()> {
        if self.path.exists() {
            remove_cgroup_dir(&self.path)?;
        }
        Ok(())
    }

    pub fn path(&self) -> &std::path::Path {
        &self.path
    }

    pub fn from_path(path: &std::path::Path) -> Self {
        Cgroup { path: path.to_path_buf() }
    }
}

fn write_file(path: &Path, content: &str) -> anyhow::Result<()> {
    fs::write(path, content.as_bytes())
        .map_err(|e| anyhow::anyhow!("failed to write {}: {}", path.display(), e))
}

fn remove_cgroup_dir(dir: &Path) -> anyhow::Result<()> {
    if dir.is_dir() {
        for entry in fs::read_dir(dir)? {
            let entry = entry?;
            let path = entry.path();
            if path.is_dir() {
                remove_cgroup_dir(&path)?;
            }
        }
    }
    fs::remove_dir(dir)?;
    Ok(())
}
