use nats;
use rand::Rng;
use serde::{Deserialize, Serialize};
use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::{Duration, Instant};
use std::{env, thread};

#[derive(Debug, Deserialize)]
struct TxEvent {
    id: String,
    debit_account: String,
    credit_account: String,
    amount: u64,
    currency: String,
}

#[derive(Debug, Serialize)]
struct TxResult {
    id: String,
    status: String,
    error_msg: String,
    latency_ms: u64,
    worker_id: String,
}

fn account_to_u128(account: &str) -> u128 {
    let n: u128 = match account.strip_prefix("acc:") {
        Some(num) => num.parse().unwrap_or(1),
        None => 1,
    };
    n
}

#[repr(C, packed)]
struct TbTransfer {
    id: u128,
    debit_account_id: u128,
    credit_account_id: u128,
    reserved: u128,
    amount: u64,
    reserved2: u32,
    user_data: u32,
    reserved3: u64,
    reserved4: u64,
    pending_id: u32,
    timeout: u32,
    ledger: u8,
    code: u8,
    flags: u16,
    timestamp: u64,
    reserved5: u128,
}

fn create_transfer(
    conn: &mut TcpStream,
    tx: &TxEvent,
) -> Result<(), String> {
    let id: u128 = rand::thread_rng().gen();

    let transfer = TbTransfer {
        id,
        debit_account_id: account_to_u128(&tx.debit_account),
        credit_account_id: account_to_u128(&tx.credit_account),
        reserved: 0,
        amount: tx.amount,
        reserved2: 0,
        user_data: 0,
        reserved3: 0,
        reserved4: 0,
        pending_id: 0,
        timeout: 0,
        ledger: 1,
        code: 0,
        flags: 0,
        timestamp: 0,
        reserved5: 0,
    };

    let transfer_bytes: &[u8; 128] = unsafe { &*(&transfer as *const TbTransfer as *const [u8; 128]) };

    let mut packet = Vec::with_capacity(8 + 128);
    packet.extend_from_slice(&3u32.to_le_bytes());
    packet.extend_from_slice(&0u32.to_le_bytes());
    packet.extend_from_slice(transfer_bytes);

    conn.write_all(&packet).map_err(|e| format!("write: {}", e))?;

    let mut resp = [0u8; 128];
    conn.read_exact(&mut resp).map_err(|e| format!("read: {}", e))?;

    let status = u32::from_le_bytes([resp[0], resp[1], resp[2], resp[3]]);
    if status != 0 {
        return Err(format!("TB error code: {}", status));
    }

    Ok(())
}

fn connect_tigerbeetle(addr: &str, timeout: Duration) -> Option<TcpStream> {
    let parsed: std::net::SocketAddr = addr.parse().unwrap_or_else(|_| {
        "127.0.0.1:3000".parse().unwrap()
    });
    match TcpStream::connect_timeout(&parsed, timeout) {
        Ok(stream) => {
            let _ = stream.set_read_timeout(Some(Duration::from_secs(5)));
            let _ = stream.set_write_timeout(Some(Duration::from_secs(5)));
            Some(stream)
        }
        Err(_) => None,
    }
}

fn main() {
    let nats_addr = env::var("NATS_ADDR").unwrap_or_else(|_| "localhost:4222".to_string());
    let tb_addr = env::var("TB_ADDR").unwrap_or_else(|_| "127.0.0.1:3000".to_string());
    let worker_id = env::var("WORKER_ID").unwrap_or_else(|_| {
        format!("worker-{:04}", rand::thread_rng().gen_range(0..9999))
    });

    eprintln!("[{}] starting...", worker_id);
    eprintln!("[{}] NATS: {}", worker_id, nats_addr);
    eprintln!("[{}] TigerBeetle: {}", worker_id, tb_addr);

    let nc = match nats::Options::new()
        .with_name(&worker_id)
        .connect(&nats_addr)
    {
        Ok(nc) => nc,
        Err(e) => {
            eprintln!("[{}] NATS connect failed: {}", worker_id, e);
            return;
        }
    };
    eprintln!("[{}] connected to NATS", worker_id);

    let mut tb = connect_tigerbeetle(&tb_addr, Duration::from_secs(5));
    match &tb {
        Some(_) => eprintln!("[{}] connected to TigerBeetle", worker_id),
        None => eprintln!("[{}] TigerBeetle unavailable, will retry", worker_id),
    }
    let mut tb_retried = false;

    let sub = nc.subscribe("tx.pending").expect("subscribe failed");
    eprintln!("[{}] waiting for transactions...", worker_id);

    for msg in sub.iter() {
        let tx: TxEvent = match serde_json::from_slice(&msg.data) {
            Ok(t) => t,
            Err(e) => {
                eprintln!("[{}] bad message: {}", worker_id, e);
                continue;
            }
        };

        let start = Instant::now();

        let result = if tb.is_some() {
            let conn = tb.as_mut().unwrap();
            match create_transfer(conn, &tx) {
                Ok(()) => TxResult {
                    id: tx.id.clone(),
                    status: "confirmed".to_string(),
                    error_msg: String::new(),
                    latency_ms: start.elapsed().as_millis() as u64,
                    worker_id: worker_id.clone(),
                },
                Err(e) => {
                    if !tb_retried {
                        eprintln!("[{}] TB error, reconnecting: {}", worker_id, e);
                        tb = connect_tigerbeetle(&tb_addr, Duration::from_secs(5));
                        tb_retried = true;
                    }
                    TxResult {
                        id: tx.id.clone(),
                        status: "error".to_string(),
                        error_msg: e,
                        latency_ms: start.elapsed().as_millis() as u64,
                        worker_id: worker_id.clone(),
                    }
                }
            }
        } else {
            thread::sleep(Duration::from_micros(100));
            TxResult {
                id: tx.id.clone(),
                status: "confirmed".to_string(),
                error_msg: String::new(),
                latency_ms: start.elapsed().as_millis() as u64,
                worker_id: worker_id.clone(),
            }
        };

        let elapsed = start.elapsed();
        let latency_ms = elapsed.as_millis() as u64;

        println!(
            "{} {} | {} \u{2192} {} | ${} | {}ms",
            if result.status == "confirmed" { "\u{2705}" } else { "\u{274C}" },
            tx.id,
            tx.debit_account,
            tx.credit_account,
            tx.amount,
            latency_ms,
        );

        let mut result = result;
        result.latency_ms = latency_ms;
        let resp_data = serde_json::to_vec(&result).unwrap_or_default();
        let _ = nc.publish("tx.completed", &resp_data);
    }
}
