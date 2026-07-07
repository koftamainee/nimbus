use std::ffi::CString;
use std::path::Path;

struct ChildContext {
    rootfs: CString,
    old_root: CString,
    workdir: CString,
    hostname: CString,
    cmd: Vec<CString>,
    env: Vec<CString>,
    stdout_fd: i32,
    stderr_fd: i32,
}

extern "C" fn child_main(arg: *mut libc::c_void) -> libc::c_int {
    let ctx = unsafe { Box::from_raw(arg as *mut ChildContext) };

    if let Err(e) = nix::mount::mount::<str, str, str, str>(
        None,
        "/",
        None,
        nix::mount::MsFlags::MS_PRIVATE | nix::mount::MsFlags::MS_REC,
        None,
    ) {
        eprintln!("make root private failed: {}", e);
        return 1;
    }

    if let Err(e) = nix::mount::mount(
        Some(ctx.rootfs.as_c_str()),
        ctx.rootfs.as_c_str(),
        None::<&std::path::Path>,
        nix::mount::MsFlags::MS_BIND | nix::mount::MsFlags::MS_REC,
        None::<&std::path::Path>,
    ) {
        eprintln!("bind mount rootfs failed: {}", e);
        return 2;
    }

    if let Err(e) = std::fs::create_dir_all(Path::new(ctx.old_root.to_str().unwrap())) {
        eprintln!("create old_root dir failed: {}", e);
        return 3;
    }

    if let Err(e) = nix::unistd::pivot_root(ctx.rootfs.as_c_str(), ctx.old_root.as_c_str()) {
        eprintln!("pivot_root failed: {}", e);
        return 4;
    }

    if let Err(e) = std::env::set_current_dir(Path::new("/")) {
        eprintln!("chdir failed: {}", e);
        return 5;
    }

    let _ = nix::mount::umount("/.forge_old_root");
    let _ = std::fs::remove_dir("/.forge_old_root");

    unsafe {
        let mut set: libc::sigset_t = std::mem::zeroed();
        libc::sigemptyset(&mut set);
        libc::sigaddset(&mut set, libc::SIGINT);
        libc::sigprocmask(libc::SIG_UNBLOCK, &set, std::ptr::null_mut());
    }

    let _ = unsafe {
        libc::setrlimit(
            libc::RLIMIT_NOFILE,
            &libc::rlimit { rlim_cur: 1024, rlim_max: 1024 },
        );
        libc::setrlimit(
            libc::RLIMIT_NPROC,
            &libc::rlimit { rlim_cur: 256, rlim_max: 256 },
        );
        libc::setrlimit(
            libc::RLIMIT_CORE,
            &libc::rlimit { rlim_cur: 0, rlim_max: 0 },
        );
        libc::setrlimit(
            libc::RLIMIT_STACK,
            &libc::rlimit { rlim_cur: 8 * 1024 * 1024, rlim_max: 8 * 1024 * 1024 },
        );
    };

    mount_fs();

    let _ = nix::unistd::sethostname(ctx.hostname.to_str().unwrap_or("forge"));

    let _ = std::env::set_current_dir(Path::new(ctx.workdir.to_str().unwrap_or("/")));

    unsafe {
        libc::dup2(ctx.stdout_fd, libc::STDOUT_FILENO);
        libc::dup2(ctx.stderr_fd, libc::STDERR_FILENO);
        libc::close(ctx.stdout_fd);
        libc::close(ctx.stderr_fd);
    }

    match nix::unistd::execvpe(&ctx.cmd[0], &ctx.cmd, &ctx.env) {
        Err(e) => {
            eprintln!("exec failed: {}", e);
            4
        }
        Ok(_) => 0,
    }
}

fn mount_fs() {
    let _ = nix::mount::mount(
        Some("proc"),
        "/proc",
        Some("proc"),
        nix::mount::MsFlags::MS_NOSUID
            | nix::mount::MsFlags::MS_NOEXEC
            | nix::mount::MsFlags::MS_NODEV,
        None::<&std::path::Path>,
    );

    let _ = nix::mount::mount(
        Some("tmpfs"),
        "/dev",
        Some("tmpfs"),
        nix::mount::MsFlags::MS_NOSUID | nix::mount::MsFlags::MS_STRICTATIME,
        None::<&std::path::Path>,
    );

    let _ = std::fs::create_dir_all("/dev/pts");
    let _ = nix::mount::mount(
        Some("devpts"),
        "/dev/pts",
        Some("devpts"),
        nix::mount::MsFlags::MS_NOSUID | nix::mount::MsFlags::MS_NOEXEC,
        Some("newinstance,ptmxmode=0666,mode=0620,gid=5"),
    );

    let _ = nix::mount::mount(
        Some("tmpfs"),
        "/run",
        Some("tmpfs"),
        nix::mount::MsFlags::MS_NOSUID | nix::mount::MsFlags::MS_NODEV,
        None::<&std::path::Path>,
    );

    let _ = std::fs::create_dir_all("/dev/shm");
    let _ = nix::mount::mount(
        Some("tmpfs"),
        "/dev/shm",
        Some("tmpfs"),
        nix::mount::MsFlags::MS_NOSUID
            | nix::mount::MsFlags::MS_NODEV
            | nix::mount::MsFlags::MS_STRICTATIME,
        None::<&std::path::Path>,
    );
}

pub fn run_child(
    rootfs: &Path,
    workdir: &str,
    hostname: &str,
    cmd: &[String],
    env: &[String],
    stdout_fd: i32,
    stderr_fd: i32,
) -> anyhow::Result<libc::pid_t> {
    let rootfs_bytes = rootfs.as_os_str().as_encoded_bytes();
    let rootfs_c = CString::new(rootfs_bytes)
        .map_err(|_| anyhow::anyhow!("invalid rootfs path"))?;

    let old_root_path = rootfs.join(".forge_old_root");
    let old_root_bytes = old_root_path.as_os_str().as_encoded_bytes();
    let old_root = CString::new(old_root_bytes)
        .map_err(|_| anyhow::anyhow!("invalid old_root path"))?;

    let workdir_c = CString::new(workdir)
        .map_err(|_| anyhow::anyhow!("invalid workdir"))?;
    let hostname_c = CString::new(hostname)
        .map_err(|_| anyhow::anyhow!("invalid hostname"))?;

    let cmd_c: Vec<CString> = cmd
        .iter()
        .map(|a| CString::new(a.as_bytes()).unwrap_or_default())
        .collect();

    let env_c: Vec<CString> = env
        .iter()
        .map(|a| CString::new(a.as_bytes()).unwrap_or_default())
        .collect();

    let ctx = Box::new(ChildContext {
        rootfs: rootfs_c,
        old_root,
        workdir: workdir_c,
        hostname: hostname_c,
        cmd: cmd_c,
        env: env_c,
        stdout_fd,
        stderr_fd,
    });

    let ctx_ptr = Box::into_raw(ctx);

    let mut stack = vec![0u8; 1024 * 1024];
    let stack_top = unsafe { stack.as_mut_ptr().add(stack.len()) };

    let flags = libc::CLONE_NEWPID | libc::CLONE_NEWNS | libc::CLONE_NEWUTS | libc::SIGCHLD;

    let pid = unsafe {
        libc::clone(
            child_main as extern "C" fn(*mut libc::c_void) -> libc::c_int,
            stack_top as *mut libc::c_void,
            flags,
            ctx_ptr as *mut libc::c_void,
        )
    };

    if pid < 0 {
        let _ = unsafe { Box::from_raw(ctx_ptr) };
        anyhow::bail!("clone failed: {}", std::io::Error::last_os_error());
    }

    Ok(pid)
}
