use std::os::unix::io::IntoRawFd;
use std::path::Path;

use chrono::Utc;

use crate::cgroup::Cgroup;
use crate::config::{ContainerConfig, ContainerStatus};
use crate::image::{pull_image, unpack_image, Manifest};
use crate::isolation;
use crate::log;

fn parse_env(envs: &[String]) -> Vec<String> {
    let mut result = Vec::new();
    for e in envs {
        if let Some((key, val)) = e.split_once('=') {
            result.push(format!("{}={}", key, val));
        } else if let Ok(val) = std::env::var(e) {
            result.push(format!("{}={}", e, val));
        }
    }
    result
}

fn resolve_env(manifest_env: &[String], cli_parsed: &[String]) -> Vec<String> {
    let mut env: Vec<String> = manifest_env.iter().cloned().collect();
    for e in cli_parsed {
        if let Some((key, _)) = e.split_once('=') {
            env.retain(|existing| !existing.starts_with(&format!("{}=", key)));
        }
        env.push(e.clone());
    }
    env
}

fn resolve_cmd(manifest: &Manifest, user_cmd: &[String]) -> Vec<String> {
    if !user_cmd.is_empty() {
        if !manifest.entrypoint.is_empty() {
            manifest
                .entrypoint
                .iter()
                .chain(user_cmd.iter())
                .cloned()
                .collect()
        } else {
            user_cmd.to_vec()
        }
    } else {
        manifest
            .entrypoint
            .iter()
            .chain(manifest.cmd.iter())
            .cloned()
            .collect()
    }
}

fn parse_memory(s: Option<&str>) -> Option<u64> {
    let s = s?;
    let s = s.trim();
    if let Some(val) = s.strip_suffix("gb").or_else(|| s.strip_suffix("GB")) {
        val.trim().parse::<f64>().ok().map(|v| (v * 1024.0) as u64)
    } else if let Some(val) = s.strip_suffix("mb").or_else(|| s.strip_suffix("MB")) {
        val.trim().parse::<f64>().ok().map(|v| v as u64)
    } else if let Some(val) = s.strip_suffix("kb").or_else(|| s.strip_suffix("KB")) {
        val.trim().parse::<f64>().ok().map(|v| (v / 1024.0) as u64)
    } else {
        s.parse::<u64>().ok().map(|v| v / (1024 * 1024))
    }
}

fn pipe_to_log(fd: std::os::unix::io::RawFd, log_path: &Path, stream: &str) {
    let mut buf = [0u8; 4096];
    loop {
        match nix::unistd::read(fd, &mut buf) {
            Ok(0) => break,
            Ok(n) => {
                let s = String::from_utf8_lossy(&buf[..n]);
                let entry = log::LogEntry {
                    log: s.to_string(),
                    stream: stream.to_string(),
                    time: chrono::Utc::now().to_rfc3339(),
                };
                if let Ok(line) = serde_json::to_string(&entry) {
                    if let Ok(mut f) = std::fs::OpenOptions::new()
                        .create(true)
                        .append(true)
                        .open(log_path)
                    {
                        use std::io::Write;
                        let _ = writeln!(f, "{}", line);
                    }
                }
                print!("{}", s);
            }
            Err(nix::errno::Errno::EINTR) => continue,
            Err(_) => break,
        }
    }
}

fn spawn_and_monitor(
    data_dir: &Path,
    config: &mut ContainerConfig,
    rootfs: &Path,
    workdir: &str,
    final_cmd: &[String],
    final_env: &[String],
) -> anyhow::Result<i32> {
    let (out_r, out_w) = nix::unistd::pipe()?;
    let (err_r, err_w) = nix::unistd::pipe()?;
    let out_w_fd = out_w.into_raw_fd();
    let err_w_fd = err_w.into_raw_fd();
    let out_r_fd = out_r.into_raw_fd();
    let err_r_fd = err_r.into_raw_fd();

    let hostname = format!("forge-{}", &config.name[..12.min(config.name.len())]);
    let pid = isolation::run_child(rootfs, workdir, &hostname, final_cmd, final_env, out_w_fd, err_w_fd)?;

    nix::unistd::close(out_w_fd)?;
    nix::unistd::close(err_w_fd)?;

    let cgroup = Cgroup::create(data_dir, &config.name, config.memory_mb, config.cpus)?;
    cgroup.add_pid(pid as u32)?;

    config.pid = Some(pid as u32);
    config.status = ContainerStatus::Running;
    config.started_at = Some(Utc::now().to_rfc3339());
    config.save(data_dir)?;

    let log_path = data_dir
        .join("containers")
        .join(&config.name)
        .join(format!("{}.log", config.name));
    let log_path_out = log_path.clone();
    let log_path_err = log_path.clone();

    let out_thread = std::thread::spawn(move || {
        pipe_to_log(out_r_fd, &log_path_out, "stdout");
        let _ = nix::unistd::close(out_r_fd);
    });
    let err_thread = std::thread::spawn(move || {
        pipe_to_log(err_r_fd, &log_path_err, "stderr");
        let _ = nix::unistd::close(err_r_fd);
    });

    let status = loop {
        match nix::sys::wait::waitpid(Some(nix::unistd::Pid::from_raw(pid)), None) {
            Ok(s) => break s,
            Err(nix::errno::Errno::EINTR) => continue,
            Err(e) => anyhow::bail!("waitpid failed: {}", e),
        }
    };

    out_thread.join().unwrap_or(());
    err_thread.join().unwrap_or(());

    let exit_code = match status {
        nix::sys::wait::WaitStatus::Exited(_, code) => code,
        nix::sys::wait::WaitStatus::Signaled(_, sig, _) => 128 + sig as i32,
        _ => -1,
    };

    config.status = ContainerStatus::Exited;
    config.pid = None;
    config.exit_code = Some(exit_code);
    config.finished_at = Some(Utc::now().to_rfc3339());
    config.save(data_dir)?;

    Ok(exit_code)
}

pub async fn run_container(
    data_dir: &Path,
    image_path: &str,
    image_registry: Option<&str>,
    name: &str,
    memory: Option<&str>,
    cpus: Option<f64>,
    cli_env: &[String],
    user_cmd: &[String],
    detached: bool,
) -> anyhow::Result<i32> {
    let container_dir = data_dir.join("containers").join(name);
    if container_dir.exists() {
        anyhow::bail!("container '{}' already exists", name);
    }
    std::fs::create_dir_all(&container_dir)?;

    let rootfs = container_dir.join("rootfs");
    std::fs::create_dir_all(&rootfs)?;

    let resolved = if Path::new(image_path).exists() {
        image_path.to_string()
    } else if let Some(registry) = image_registry {
        let images_dir = data_dir.join("images");
        pull_image(image_path, registry, &images_dir).await?
    } else {
        image_path.to_string()
    };

    let manifest = unpack_image(&resolved, &container_dir)?;

    let final_cmd = resolve_cmd(&manifest, user_cmd);
    let cli_parsed = parse_env(cli_env);
    let final_env = resolve_env(&manifest.env, &cli_parsed);
    let workdir = manifest.workdir.clone();
    let memory_mb = parse_memory(memory);

    let now = Utc::now().to_rfc3339();
    let cgroup_path = format!("/sys/fs/cgroup/forge/{}", name);
    let mut config = ContainerConfig {
        name: name.to_string(),
        image: image_path.to_string(),
        cmd: final_cmd.clone(),
        status: ContainerStatus::Created,
        pid: None,
        created_at: now.clone(),
        started_at: None,
        finished_at: None,
        exit_code: None,
        cgroup_path,
        memory_mb,
        cpus,
        env: final_env.clone(),
    };
    config.save(data_dir)?;

    if detached {
        match unsafe { libc::fork() } {
            -1 => anyhow::bail!("fork failed"),
            0 => {
                let ec = spawn_and_monitor(data_dir, &mut config, &rootfs, &workdir, &final_cmd, &final_env)
                    .unwrap_or(-1);
                std::process::exit(ec);
            }
            _ => {
                println!("{}", name);
                Ok(0)
            }
        }
    } else {
        let mut old_mask = std::mem::MaybeUninit::<libc::sigset_t>::uninit();
        unsafe {
            let mut set: libc::sigset_t = std::mem::zeroed();
            libc::sigemptyset(&mut set);
            libc::sigaddset(&mut set, libc::SIGINT);
            libc::sigprocmask(libc::SIG_BLOCK, &set, old_mask.as_mut_ptr());
        }

        let ec = spawn_and_monitor(data_dir, &mut config, &rootfs, &workdir, &final_cmd, &final_env)?;

        unsafe {
            libc::sigprocmask(libc::SIG_SETMASK, old_mask.as_ptr(), std::ptr::null_mut());
        }

        println!("container {} exited with code {}", name, ec);
        Ok(ec)
    }
}

pub fn start_container(data_dir: &Path, name: &str) -> anyhow::Result<String> {
    let mut config = ContainerConfig::load(data_dir, name)?;
    if config.status == ContainerStatus::Running {
        anyhow::bail!("container {} is already running", name);
    }

    let rootfs = data_dir.join("containers").join(name).join("rootfs");
    if !rootfs.exists() {
        anyhow::bail!("rootfs for container {} not found", name);
    }

    let old_cg = Cgroup::from_path(&std::path::PathBuf::from(&config.cgroup_path));
    let _ = old_cg.remove();

    let env = config.env.clone();
    let cname = config.name.clone();

    let manifest = Manifest {
        entrypoint: vec![],
        cmd: config.cmd.clone(),
        env: config.env.clone(),
        workdir: "/".to_string(),
    };

    let final_cmd = resolve_cmd(&manifest, &[]);

    match unsafe { libc::fork() } {
        -1 => anyhow::bail!("fork failed"),
        0 => {
            let ec = spawn_and_monitor(data_dir, &mut config, &rootfs, "/", &final_cmd, &env)
                .unwrap_or(-1);
            std::process::exit(ec);
        }
        _ => Ok(cname),
    }
}

fn pid_matches_cgroup(pid: nix::unistd::Pid, cgroup_path: &str) -> bool {
    let suffix = cgroup_path.trim_start_matches("/sys/fs/cgroup");
    let path = format!("/proc/{}/cgroup", pid);
    let content = match std::fs::read_to_string(&path) {
        Ok(c) => c,
        Err(_) => return false,
    };
    content.contains(suffix)
}

fn process_is_alive(pid: nix::unistd::Pid, cgroup_path: &str) -> bool {
    let status_path = format!("/proc/{}/status", pid);
    let content = match std::fs::read_to_string(&status_path) {
        Ok(c) => c,
        Err(_) => return false,
    };
    let mut found_state = false;
    for line in content.lines() {
        if let Some(state) = line.strip_prefix("State:") {
            let state = state.trim();
            if state == "Z (zombie)" || state == "X (dead)" {
                return false;
            }
            found_state = true;
            break;
        }
    }
    if !found_state {
        return false;
    }
    pid_matches_cgroup(pid, cgroup_path)
}

pub fn stop_container(data_dir: &Path, name: &str, timeout: u64) -> anyhow::Result<()> {
    let mut config = ContainerConfig::load(data_dir, name)?;
    if config.status != ContainerStatus::Running {
        anyhow::bail!("container {} is not running", name);
    }
    let pid_raw = config.pid.unwrap_or(0) as i32;
    if pid_raw <= 0 {
        anyhow::bail!("container {} has no PID", name);
    }

    let pid = nix::unistd::Pid::from_raw(pid_raw);
    if !process_is_alive(pid, &config.cgroup_path) {
        config.status = ContainerStatus::Exited;
        config.pid = None;
        config.finished_at = Some(Utc::now().to_rfc3339());
        return config.save(data_dir);
    }

    let _ = nix::sys::signal::kill(pid, nix::sys::signal::Signal::SIGTERM);

    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(timeout);
    while std::time::Instant::now() < deadline {
        if !process_is_alive(pid, &config.cgroup_path) {
            config.status = ContainerStatus::Exited;
            config.pid = None;
            config.exit_code = config.exit_code.or(Some(143));
            config.finished_at = Some(Utc::now().to_rfc3339());
            return config.save(data_dir);
        }
        std::thread::sleep(std::time::Duration::from_millis(200));
    }

    if process_is_alive(pid, &config.cgroup_path) {
        let _ = nix::sys::signal::kill(pid, nix::sys::signal::Signal::SIGKILL);
    }

    config.status = ContainerStatus::Exited;
    config.pid = None;
    config.exit_code = config.exit_code.or(Some(137));
    config.finished_at = Some(Utc::now().to_rfc3339());
    config.save(data_dir)
}

pub fn kill_container(data_dir: &Path, name: &str) -> anyhow::Result<()> {
    let mut config = ContainerConfig::load(data_dir, name)?;
    if config.status != ContainerStatus::Running {
        anyhow::bail!("container {} is not running", name);
    }
    let pid_raw = config.pid.unwrap_or(0) as i32;
    if pid_raw <= 0 {
        anyhow::bail!("container {} has no PID", name);
    }

    let pid = nix::unistd::Pid::from_raw(pid_raw);
    if !process_is_alive(pid, &config.cgroup_path) {
        config.status = ContainerStatus::Exited;
        config.pid = None;
        config.finished_at = Some(Utc::now().to_rfc3339());
        return config.save(data_dir);
    }
    let _ = nix::sys::signal::kill(pid, nix::sys::signal::Signal::SIGKILL);

    config.status = ContainerStatus::Exited;
    config.pid = None;
    config.exit_code = Some(137);
    config.finished_at = Some(Utc::now().to_rfc3339());
    config.save(data_dir)
}

pub fn list_containers(data_dir: &Path, all: bool) -> anyhow::Result<Vec<ContainerConfig>> {
    let containers_dir = data_dir.join("containers");
    if !containers_dir.exists() {
        return Ok(vec![]);
    }

    let mut result = Vec::new();
    for entry in std::fs::read_dir(&containers_dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy().to_string();
        match ContainerConfig::load(data_dir, &name) {
            Ok(config) => {
                if config.status == ContainerStatus::Running {
                    let p = config.pid.unwrap_or(0) as i32;
                    if p > 0 {
                        let alive = process_is_alive(nix::unistd::Pid::from_raw(p), &config.cgroup_path);
                        if !alive {
                            let mut c = config.clone();
                            c.status = ContainerStatus::Exited;
                            c.pid = None;
                            c.exit_code = c.exit_code.or(Some(-1));
                            let _ = c.save(data_dir);
                        }
                    }
                }
                if all || config.status == ContainerStatus::Running {
                    result.push(config);
                }
            }
            Err(_) => continue,
        }
    }

    result.sort_by(|a, b| a.created_at.cmp(&b.created_at));
    Ok(result)
}

pub fn remove_container(data_dir: &Path, name: &str) -> anyhow::Result<()> {
    let config = ContainerConfig::load(data_dir, name)?;
    if config.status == ContainerStatus::Running {
        anyhow::bail!("cannot remove running container {}", name);
    }

    let cg = Cgroup::from_path(&std::path::PathBuf::from(&config.cgroup_path));
    let _ = cg.remove();

    let container_dir = data_dir.join("containers").join(name);
    if container_dir.exists() {
        std::fs::remove_dir_all(&container_dir)?;
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn manifest(e: &[&str], c: &[&str], w: &str) -> Manifest {
        Manifest {
            entrypoint: e.iter().map(|s| s.to_string()).collect(),
            cmd: c.iter().map(|s| s.to_string()).collect(),
            env: vec![],
            workdir: w.to_string(),
        }
    }

    #[test]
    fn test_parse_env_key_value() {
        let envs = vec!["FOO=bar".to_string(), "BAZ=qux".to_string()];
        let result = parse_env(&envs);
        assert_eq!(result, vec!["FOO=bar", "BAZ=qux"]);
    }

    #[test]
    fn test_parse_env_inherit_from_host() {
        std::env::set_var("FORGE_TEST_VAR", "test_value");
        let envs = vec!["FORGE_TEST_VAR".to_string()];
        let result = parse_env(&envs);
        assert_eq!(result, vec!["FORGE_TEST_VAR=test_value"]);
    }

    #[test]
    fn test_parse_env_inherit_missing() {
        let envs = vec!["NONEXISTENT_VAR_12345".to_string()];
        let result = parse_env(&envs);
        assert!(result.is_empty());
    }

    #[test]
    fn test_parse_env_mixed() {
        std::env::set_var("HOME", "/root");
        let envs = vec!["MY_VAR=hello".to_string(), "HOME".to_string()];
        let result = parse_env(&envs);
        assert_eq!(result, vec!["MY_VAR=hello", "HOME=/root"]);
    }

    #[test]
    fn test_parse_env_empty() {
        let result = parse_env(&[]);
        assert!(result.is_empty());
    }

    #[test]
    fn test_parse_env_empty_value() {
        let envs = vec!["FOO=".to_string()];
        let result = parse_env(&envs);
        assert_eq!(result, vec!["FOO="]);
    }

    #[test]
    fn test_resolve_env_no_cli() {
        let manifest = vec!["PATH=/usr/bin".to_string(), "HOME=/root".to_string()];
        let result = resolve_env(&manifest, &[]);
        assert_eq!(result, vec!["PATH=/usr/bin", "HOME=/root"]);
    }

    #[test]
    fn test_resolve_env_override() {
        let manifest = vec!["PATH=/usr/bin".to_string(), "HOME=/root".to_string()];
        let cli = vec!["PATH=/custom/bin".to_string()];
        let result = resolve_env(&manifest, &cli);
        assert_eq!(result, vec!["HOME=/root", "PATH=/custom/bin"]);
    }

    #[test]
    fn test_resolve_env_add_new() {
        let manifest = vec!["PATH=/usr/bin".to_string()];
        let cli = vec!["MY_VAR=hello".to_string()];
        let result = resolve_env(&manifest, &cli);
        assert_eq!(result, vec!["PATH=/usr/bin", "MY_VAR=hello"]);
    }

    #[test]
    fn test_resolve_env_override_multiple() {
        let manifest = vec![
            "A=1".to_string(),
            "B=2".to_string(),
            "C=3".to_string(),
        ];
        let cli = vec!["A=override".to_string(), "D=new".to_string()];
        let result = resolve_env(&manifest, &cli);
        assert_eq!(result, vec!["B=2", "C=3", "A=override", "D=new"]);
    }

    #[test]
    fn test_resolve_env_all_overridden() {
        let manifest = vec!["KEY=old".to_string()];
        let cli = vec!["KEY=new".to_string()];
        let result = resolve_env(&manifest, &cli);
        assert_eq!(result, vec!["KEY=new"]);
    }

    #[test]
    fn test_resolve_cmd_user_cmd_only() {
        let m = manifest(&[], &[], "/");
        let user = vec!["echo".to_string(), "hello".to_string()];
        assert_eq!(resolve_cmd(&m, &user), vec!["echo", "hello"]);
    }

    #[test]
    fn test_resolve_cmd_entrypoint_only() {
        let m = manifest(&["/app/server"], &[], "/");
        assert_eq!(resolve_cmd(&m, &[]), vec!["/app/server"]);
    }

    #[test]
    fn test_resolve_cmd_entrypoint_and_cmd() {
        let m = manifest(&["/app/server"], &["--port", "8080"], "/");
        assert_eq!(resolve_cmd(&m, &[]), vec!["/app/server", "--port", "8080"]);
    }

    #[test]
    fn test_resolve_cmd_entrypoint_with_user_cmd() {
        let m = manifest(&["/app/server"], &["--port", "8080"], "/");
        let user = vec!["--debug".to_string()];
        assert_eq!(resolve_cmd(&m, &user), vec!["/app/server", "--debug"]);
    }

    #[test]
    fn test_resolve_cmd_empty_everything() {
        let m = manifest(&[], &[], "/");
        assert!(resolve_cmd(&m, &[]).is_empty());
    }

    #[test]
    fn test_resolve_cmd_user_overrides_cmd() {
        let m = manifest(&[], &["default_cmd"], "/");
        let user = vec!["user_cmd".to_string()];
        assert_eq!(resolve_cmd(&m, &user), vec!["user_cmd"]);
    }

    #[test]
    fn test_parse_memory_none() {
        assert_eq!(parse_memory(None), None);
    }

    #[test]
    fn test_parse_memory_mb() {
        assert_eq!(parse_memory(Some("512mb")), Some(512));
    }

    #[test]
    fn test_parse_memory_mb_uppercase() {
        assert_eq!(parse_memory(Some("256MB")), Some(256));
    }

    #[test]
    fn test_parse_memory_gb() {
        assert_eq!(parse_memory(Some("2gb")), Some(2048));
    }

    #[test]
    fn test_parse_memory_gb_uppercase() {
        assert_eq!(parse_memory(Some("1GB")), Some(1024));
    }

    #[test]
    fn test_parse_memory_gb_float() {
        assert_eq!(parse_memory(Some("1.5gb")), Some(1536));
    }

    #[test]
    fn test_parse_memory_kb_small() {
        assert_eq!(parse_memory(Some("1024kb")), Some(1));
    }

    #[test]
    fn test_parse_memory_kb_rounds_down() {
        assert_eq!(parse_memory(Some("1kb")), Some(0));
    }

    #[test]
    fn test_parse_memory_raw_bytes() {
        let result = parse_memory(Some("104857600"));
        assert_eq!(result, Some(100));
    }

    #[test]
    fn test_parse_memory_invalid() {
        assert_eq!(parse_memory(Some("not_a_number")), None);
    }

    #[test]
    fn test_parse_memory_with_spaces() {
        assert_eq!(parse_memory(Some("  512mb  ")), Some(512));
    }

    #[test]
    fn test_stop_container_not_running() {
        let dir = tempfile::TempDir::new().unwrap();
        let name = "stop-not-running-test";
        let cfg = ContainerConfig {
            name: name.to_string(),
            image: "test.tar".to_string(),
            cmd: vec!["sh".to_string()],
            status: ContainerStatus::Exited,
            pid: None,
            created_at: "".to_string(),
            started_at: None,
            finished_at: None,
            exit_code: Some(0),
            cgroup_path: "/sys/fs/cgroup/forge/test".to_string(),
            memory_mb: None,
            cpus: None,
            env: vec![],
        };
        cfg.save(dir.path()).unwrap();
        let result = stop_container(dir.path(), name, 10);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("not running"));
    }

    #[test]
    fn test_kill_container_not_running() {
        let dir = tempfile::TempDir::new().unwrap();
        let name = "kill-not-running-test";
        let cfg = ContainerConfig {
            name: name.to_string(),
            image: "test.tar".to_string(),
            cmd: vec!["sh".to_string()],
            status: ContainerStatus::Created,
            pid: None,
            created_at: "".to_string(),
            started_at: None,
            finished_at: None,
            exit_code: None,
            cgroup_path: "/sys/fs/cgroup/forge/test".to_string(),
            memory_mb: None,
            cpus: None,
            env: vec![],
        };
        cfg.save(dir.path()).unwrap();
        let result = kill_container(dir.path(), name);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("not running"));
    }

    #[test]
    fn test_remove_container_running_fails() {
        let dir = tempfile::TempDir::new().unwrap();
        let name = "remove-running-test";
        let cfg = ContainerConfig {
            name: name.to_string(),
            image: "test.tar".to_string(),
            cmd: vec!["sh".to_string()],
            status: ContainerStatus::Running,
            pid: Some(99999),
            created_at: "".to_string(),
            started_at: None,
            finished_at: None,
            exit_code: None,
            cgroup_path: "/sys/fs/cgroup/forge/test".to_string(),
            memory_mb: None,
            cpus: None,
            env: vec![],
        };
        cfg.save(dir.path()).unwrap();
        let result = remove_container(dir.path(), name);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("running"));
    }

    #[test]
    fn test_remove_container_not_found() {
        let dir = tempfile::TempDir::new().unwrap();
        let result = remove_container(dir.path(), "nonexistent");
        assert!(result.is_err());
    }

    #[test]
    fn test_list_containers_empty() {
        let dir = tempfile::TempDir::new().unwrap();
        let containers = list_containers(dir.path(), true).unwrap();
        assert!(containers.is_empty());
    }
}
