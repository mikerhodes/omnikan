use anyhow::{Context, Result};
use std::io::Write;
use std::process::{Command, Stdio};

pub fn execute_script(source: &[u8], args: &[u8]) -> Result<Vec<u8>> {
    let mut child = Command::new("/usr/bin/osascript")
        .args(["-l", "JavaScript"])
        .env(
            "OSA_ARGS",
            std::str::from_utf8(args).context("invalid OSA_ARGS")?,
        )
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .context("starting osascript")?;
    child
        .stdin
        .take()
        .context("opening osascript stdin")?
        .write_all(source)?;
    let output = child.wait_with_output().context("waiting for osascript")?;
    if !output.status.success() {
        anyhow::bail!("osascript failed: {}", output.status);
    }
    Ok(output.stdout)
}
