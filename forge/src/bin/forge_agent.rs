use std::path::PathBuf;
use std::time::Duration;

use anyhow::Context;
use chrono::Utc;
use clap::Parser;
use tonic::transport::{Channel, Endpoint};

use forge::container;

pub mod quorum {
    pub mod v1 {
        tonic::include_proto!("quorum.v1");
    }
}

use quorum::v1::kv_client::KvClient;
use quorum::v1::watch_client::WatchClient;
use quorum::v1::{PutRequest, WatchRequest};
use quorum::v1::event::EventType;

type Kv = KvClient<Channel>;
type Watch = WatchClient<Channel>;

#[derive(Parser)]
#[command(name = "forge-agent", version, about = "Forge node agent")]
struct AgentCli {
    #[arg(long)]
    node_id: String,

    #[arg(long, default_value = "http://127.0.0.1:9090")]
    quorum_addr: String,

    #[arg(long, default_value = "/var/lib/forge")]
    data_dir: String,

    #[arg(long, default_value = "http://127.0.0.1:9091")]
    image_addr: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = AgentCli::parse();
    let data_dir = PathBuf::from(&cli.data_dir);

    let endpoint = Endpoint::from_shared(cli.quorum_addr.clone())?
        .connect_timeout(Duration::from_secs(5));

    let mut kv_client: Kv = KvClient::connect(endpoint.clone()).await
        .context("connect to quorum kv")?;

    let node_info = serde_json::json!({
        "id": cli.node_id,
        "hostname": hostname(),
        "addr": "",
    });
    kv_client.put(PutRequest {
        key: format!("/nodes/{}", cli.node_id).into_bytes(),
        value: node_info.to_string().into_bytes(),
    }).await?;

    let kv_client_hb: Kv = KvClient::connect(endpoint.clone()).await?;
    let node_id_hb = cli.node_id.clone();
    tokio::spawn(async move {
        heartbeat_loop(kv_client_hb, &node_id_hb).await;
    });

    let mut watch_client: Watch = WatchClient::connect(endpoint).await?;
    let prefix = format!("/nodes/{}/assignments/", cli.node_id);

    eprintln!("agent {} listening for assignments at {}", cli.node_id, prefix);

    loop {
        match watch_assignment_loop(
            &mut watch_client,
            &mut kv_client,
            &prefix,
            &data_dir,
            &cli.image_addr,
            &cli.node_id,
        )
        .await
        {
            Ok(()) => {
                tokio::time::sleep(Duration::from_secs(1)).await;
                watch_client = WatchClient::connect(
                    Endpoint::from_shared(cli.quorum_addr.clone())?
                        .connect_timeout(Duration::from_secs(5)),
                )
                .await?;
                kv_client = KvClient::connect(
                    Endpoint::from_shared(cli.quorum_addr.clone())?
                        .connect_timeout(Duration::from_secs(5)),
                )
                .await?;
            }
            Err(e) => {
                eprintln!("watch error: {}, reconnecting in 1s", e);
                tokio::time::sleep(Duration::from_secs(1)).await;
                watch_client = WatchClient::connect(
                    Endpoint::from_shared(cli.quorum_addr.clone())?
                        .connect_timeout(Duration::from_secs(5)),
                )
                .await?;
                kv_client = KvClient::connect(
                    Endpoint::from_shared(cli.quorum_addr.clone())?
                        .connect_timeout(Duration::from_secs(5)),
                )
                .await?;
            }
        }
    }
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
    data_dir: &PathBuf,
    image_addr: &str,
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

                    let image_name = spec["image"].as_str().unwrap_or("unknown");
                    let image_path = download_image(image_name, image_addr).await;

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
                    let detached = true;

                    let ec = container::run_container(
                        data_dir,
                        &image_path,
                        &container_name,
                        memory.as_deref(),
                        cpus,
                        &env,
                        &cmd,
                        detached,
                    );

                    let status = match &ec {
                        Ok(_) => "running",
                        Err(e) => {
                            eprintln!("failed to start container {}: {}", container_name, e);
                            "exited"
                        }
                    };

                    let ec_val: Option<i32> = match ec {
                        Ok(code) => Some(code),
                        Err(_) => Some(-1),
                    };

                    let container_status = serde_json::json!({
                        "container_name": container_name,
                        "node_id": node_id,
                        "status": status,
                        "pid": 0,
                        "exit_code": ec_val,
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
                    container::kill_container(data_dir, &name).ok();
                    container::remove_container(data_dir, &name).ok();
                }
            }
        }
    }

    Ok(())
}

async fn download_image(name: &str, image_addr: &str) -> String {
    let images_dir = "/var/lib/forge/images";
    tokio::fs::create_dir_all(images_dir).await.unwrap_or_default();
    let dest = format!("{}/{}.tar", images_dir, name);

    if std::path::Path::new(&dest).exists() {
        return dest;
    }

    let url = format!("{}/images/{}", image_addr.trim_end_matches('/'), name);
    eprintln!("downloading image from {}", url);

    match reqwest::get(&url).await {
        Ok(resp) => {
            if let Err(e) = resp.error_for_status_ref() {
                eprintln!("image download failed: {}", e);
                return dest;
            }
            use futures_util::StreamExt;
            let mut file = tokio::fs::File::create(&dest).await.unwrap();
            let mut stream = resp.bytes_stream();
            while let Some(chunk) = stream.next().await {
                if let Ok(bytes) = chunk {
                    use tokio::io::AsyncWriteExt;
                    file.write_all(&bytes).await.unwrap();
                }
            }
            eprintln!("image downloaded: {}", dest);
        }
        Err(e) => {
            eprintln!("image download request failed: {}", e);
        }
    }

    dest
}

fn hostname() -> String {
    std::env::var("HOSTNAME").unwrap_or_else(|_| "unknown".to_string())
}
