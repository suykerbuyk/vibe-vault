// Rust fixture for verify symbol-grammar tests.

pub struct CanMonitor {
    name: String,
}

pub enum FrameKind {
    Data,
    Remote,
}

pub const MAX_FRAMES: usize = 128;

impl CanMonitor {
    pub fn new(name: String) -> Self {
        Self { name }
    }
}

pub fn run_single_frame_mode(cfg: &str) -> Result<(), String> {
    Ok(())
}

pub async fn run_replay_mode(file: &str) -> Result<(), String> {
    Ok(())
}

fn load_config() -> Result<String, String> {
    Ok(String::new())
}

macro_rules! trace_call {
    ($x:expr) => { println!("{}", $x) };
}
