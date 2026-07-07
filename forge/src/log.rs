use chrono::Utc;
use std::fs;
use std::io::{BufRead, BufReader, Read, Seek, SeekFrom};
use std::path::Path;

#[derive(Debug, serde::Serialize)]
pub struct LogEntry {
    pub log: String,
    pub stream: String,
    pub time: String,
}

fn log_path(data_dir: &Path, name: &str) -> std::path::PathBuf {
    data_dir
        .join("containers")
        .join(name)
        .join(format!("{}.log", name))
}

pub fn write_log(data_dir: &Path, name: &str, stream: &str, message: &str) -> anyhow::Result<()> {
    let path = log_path(data_dir, name);
    let entry = LogEntry {
        log: message.to_string(),
        stream: stream.to_string(),
        time: Utc::now().to_rfc3339(),
    };
    let mut line = serde_json::to_string(&entry)?;
    line.push('\n');
    let mut file = fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)?;
    use std::io::Write;
    file.write_all(line.as_bytes())?;
    Ok(())
}

pub fn read_logs(
    data_dir: &Path,
    name: &str,
    tail: Option<usize>,
    follow: bool,
) -> anyhow::Result<()> {
    let path = log_path(data_dir, name);

    let file = fs::File::open(&path)
        .map_err(|e| anyhow::anyhow!("cannot open log for {}: {}", name, e))?;
    let mut reader = BufReader::new(file);

    if let Some(n) = tail {
        let lines = read_last_lines(&mut reader, n)?;
        for line in &lines {
            print_line(line);
        }
    } else {
        for line in reader.by_ref().lines() {
            let line = line?;
            print_line(&line);
        }
    }

    if follow {
        let end = reader.get_ref().metadata()?.len();
        reader.get_mut().seek(SeekFrom::Start(end))?;

        loop {
            for line in reader.by_ref().lines() {
                match line {
                    Ok(l) => print_line(&l),
                    Err(_) => break,
                }
            }
            std::thread::sleep(std::time::Duration::from_millis(200));
        }
    }

    Ok(())
}

fn print_line(line: &str) {
    if let Ok(entry) = serde_json::from_str::<serde_json::Value>(line) {
        let msg = entry["log"].as_str().unwrap_or(line);
        let stream = entry["stream"].as_str().unwrap_or("unknown");
        let prefix = if stream == "stderr" { "[stderr] " } else { "" };
        print!("{}{}", prefix, msg);
    } else {
        println!("{}", line);
    }
}

fn read_last_lines<R: Read + Seek>(
    reader: &mut BufReader<R>,
    n: usize,
) -> anyhow::Result<Vec<String>> {
    let lines: Vec<String> = reader.lines().filter_map(|l| l.ok()).collect();
    if lines.len() > n {
        Ok(lines[lines.len() - n..].to_vec())
    } else {
        Ok(lines)
    }
}
