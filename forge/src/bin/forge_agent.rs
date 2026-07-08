use std::collections::HashMap;
use std::time::Duration;

use chrono::Utc;
use clap::Parser;
use hyper_util::rt::TokioIo;
use tonic::transport::{Channel, Endpoint, Uri};
use tonic::Code;
use tower::service_fn;
use tokio::net::UnixStream;

pub mod quorum {
    pub mod v1 {
        tonic::include_proto!("quorum.v1");
    }
}

pub mod nimbus {
    pub mod v1 {
        tonic::include_proto!("nimbus.v1");
    }
}

use quorum::v1::kv_client::KvClient;
use quorum::v1::watch_client::WatchClient;
use quorum::v1::{PutRequest, WatchRequest};
use quorum::v1::event::EventType;

use nimbus::v1::forge_runtime_client::ForgeRuntimeClient;
use nimbus::v1::{KillRequest, RemoveRequest, RunRequest, StopRequest};

type Kv = KvClient<Channel>;
type Watch = WatchClient<Channel>;

#[derive(Parser)]
#[command(name = "forge-agent", version, about = "Forge node agent")]
struct AgentCli {
    #[arg(long)]
    node_id: String,

    #[arg(long, default_value = "")]
    db_cluster: String,

    #[arg(long, default_value = "/var/run/forge.sock")]
    forge_socket: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = AgentCli::parse();

    let cluster = parse_cluster(&cli.db_cluster);

    let channel = connect_to_leader(&cluster).await?;

    let mut kv_client: Kv = KvClient::new(channel.clone());

    let node_info = serde_json::json!({
        "id": cli.node_id,
        "hostname": hostname(),
        "addr": "",
    });
    kv_client.put(PutRequest {
        key: format!("/nodes/{}", cli.node_id).into_bytes(),
        value: node_info.to_string().into_bytes(),
    }).await?;

    let kv_client_hb: Kv = KvClient::new(channel.clone());
    let node_id_hb = cli.node_id.clone();
    tokio::spawn(async move {
        heartbeat_loop(kv_client_hb, &node_id_hb).await;
    });

    let mut watch_client: Watch = WatchClient::new(channel.clone());
    let prefix = format!("/nodes/{}/assignments/", cli.node_id);

    eprintln!("agent {} listening for assignments at {}", cli.node_id, prefix);

    loop {
        match watch_assignment_loop(
            &mut watch_client,
            &mut kv_client,
            &prefix,
            &cli.forge_socket,
            &cli.node_id,
        )
        .await
        {
            Ok(()) => {
                tokio::time::sleep(Duration::from_secs(1)).await;
                let ch = connect_to_leader(&cluster).await.unwrap();
                watch_client = WatchClient::new(ch.clone());
                kv_client = KvClient::new(ch);
            }
            Err(e) => {
                eprintln!("watch error: {}, reconnecting in 1s", e);
                tokio::time::sleep(Duration::from_secs(1)).await;
                let ch = connect_to_leader(&cluster).await.unwrap();
                watch_client = WatchClient::new(ch.clone());
                kv_client = KvClient::new(ch);
            }
        }
    }
}

fn normalize_addr(addr: &str) -> String {
    if addr.starts_with(':') {
        format!("127.0.0.1{}", addr)
    } else {
        addr.to_string()
    }
}

async fn connect_to_leader(cluster: &HashMap<String, String>) -> anyhow::Result<Channel> {
    let addrs: Vec<String> = cluster.values().map(|s| normalize_addr(s)).collect();
    for addr in &addrs {
        eprintln!("connecting to quorum-db at {}", addr);
        let endpoint = Endpoint::from_shared(format!("http://{}", addr))?
            .connect_timeout(Duration::from_secs(3));
        match endpoint.connect().await {
            Ok(ch) => {
                let mut kv = KvClient::new(ch.clone());
                let req = PutRequest {
                    key: b"/internal/health".to_vec(),
                    value: b"ping".to_vec(),
                };
                match kv.put(req).await {
                    Ok(_) => return Ok(ch),
                    Err(status) => {
                        if status.code() == Code::Unavailable
                            && status.message().starts_with("leader: ")
                        {
                            let leader_id = status.message().strip_prefix("leader: ").unwrap_or("");
                            eprintln!("redirected to leader {}", leader_id);
                            if let Some(leader_addr) = cluster.get(leader_id) {
                                let leader_ep = Endpoint::from_shared(format!("http://{}", normalize_addr(leader_addr)))?
                                    .connect_timeout(Duration::from_secs(3));
                                match leader_ep.connect().await {
                                    Ok(lch) => return Ok(lch),
                                    Err(e) => {
                                        eprintln!("connect to leader {} failed: {}", leader_addr, e);
                                        continue;
                                    }
                                }
                            }
                        } else {
                            eprintln!("health check failed at {}: {}", addr, status.message());
                        }
                    }
                }
            }
            Err(e) => {
                eprintln!("connection failed at {}: {}", addr, e);
            }
        }
    }
    anyhow::bail!("could not connect to any quorum-db node");
}

fn parse_cluster(cluster: &str) -> HashMap<String, String> {
    let mut result = HashMap::new();
    for part in cluster.split(',') {
        let part = part.trim();
        if part.is_empty() {
            continue;
        }
        if let Some(eq) = part.find('=') {
            let id = part[..eq].to_string();
            let addr = part[eq + 1..].to_string();
            result.insert(id, addr);
        }
    }
    result
}

async fn connect_forged(socket: &str) -> anyhow::Result<ForgeRuntimeClient<Channel>> {
    let socket = socket.to_string();
    let channel = Endpoint::try_from("http://[::]:0")?
        .connect_with_connector(service_fn(move |_: Uri| {
            let socket = socket.clone();
            async move {
                let stream = UnixStream::connect(&socket).await?;
                Ok::<_, std::io::Error>(TokioIo::new(stream))
            }
        }))
        .await?;
    Ok(ForgeRuntimeClient::new(channel))
}

async fn heartbeat_loop(mut client: Kv, node_id: &str) {
    loop {
        tokio::time::sleep(Duration::from_secs(5)).await;
        let hb = serde_json::json!({ "time": Utc::now().to_rfc3339() });
        if let Err(e) = client
            .put(PutRequest {
                key: format!("/nodes/{}/heartbeat", node_id).into_bytes(),
                value: hb.to_string().into_bytes(),
            })
            .await
        {
            eprintln!("heartbeat error: {}", e);
        }
    }
}

async fn watch_assignment_loop(
    watch_client: &mut Watch,
    kv_client: &mut Kv,
    prefix: &str,
    forge_socket: &str,
    node_id: &str,
) -> anyhow::Result<()> {
    let mut stream = watch_client
        .watch(WatchRequest {
            key: prefix.as_bytes().to_vec(),
            start_revision: 0,
        })
        .await?
        .into_inner();

    while let Some(resp) = stream.message().await? {
        for event in resp.events {
            let event_type = event.r#type();
            match event_type {
                EventType::Put => {
                    let val = String::from_utf8_lossy(&event.value);
                    let assignment: serde_json::Value = serde_json::from_str(&val)?;
                    let container_name = assignment["container_name"]
                        .as_str()
                        .unwrap_or("unknown")
                        .to_string();
                    let spec = &assignment["spec"];

                    eprintln!("assignment: starting container {}", container_name);

                    let mut forged = connect_forged(forge_socket).await?;

                    let image_name = spec["image"].as_str().unwrap_or("unknown");
                    let memory = spec["memory"].as_str().map(|s| s.to_string());
                    let cpus = spec["cpus"].as_f64();
                    let env: Vec<String> = spec["env"]
                        .as_array()
                        .map(|a| {
                            a.iter()
                                .filter_map(|v| v.as_str().map(|s| s.to_string()))
                                .collect()
                        })
                        .unwrap_or_default();
                    let cmd: Vec<String> = spec["cmd"]
                        .as_array()
                        .map(|a| {
                            a.iter()
                                .filter_map(|v| v.as_str().map(|s| s.to_string()))
                                .collect()
                        })
                        .unwrap_or_default();

                    let req = RunRequest {
                        image: image_name.to_string(),
                        name: container_name.clone(),
                        memory: memory.unwrap_or_default(),
                        cpus: cpus.unwrap_or(0.0),
                        env,
                        cmd,
                    };

                    let result = forged.run(req).await;

                    let status = match &result {
                        Ok(_) => "running",
                        Err(e) => {
                            eprintln!("failed to start container {}: {}", container_name, e);
                            "exited"
                        }
                    };

                    let container_status = serde_json::json!({
                        "container_name": container_name,
                        "node_id": node_id,
                        "status": status,
                        "pid": 0,
                        "exit_code": 0,
                        "started_at": Utc::now().to_rfc3339(),
                        "finished_at": serde_json::Value::Null,
                    });

                    kv_client
                        .put(PutRequest {
                            key: format!("/containers/{}/status", container_name)
                                .into_bytes(),
                            value: container_status.to_string().into_bytes(),
                        })
                        .await
                        .ok();
                }
                EventType::Delete => {
                    let key = String::from_utf8_lossy(&event.key).to_string();
                    let name = key
                        .strip_prefix(prefix)
                        .unwrap_or(&key)
                        .to_string();
                    eprintln!("assignment removed: stopping container {}", name);

                    let forged_socket = forge_socket.to_string();
                    tokio::spawn(async move {
                        match connect_forged(&forged_socket).await {
                            Ok(mut forged) => {
                                forged.stop(StopRequest { name: name.clone(), timeout: 10 }).await.ok();
                                forged.kill(KillRequest { name: name.clone() }).await.ok();
                                forged.remove(RemoveRequest { name }).await.ok();
                            }
                            Err(e) => eprintln!("stop {}: failed to connect to forged: {}", name, e),
                        }
                    });
                }
            }
        }
    }

    Ok(())
}

fn hostname() -> String {
    std::env::var("HOSTNAME").unwrap_or_else(|_| "unknown".to_string())
}
