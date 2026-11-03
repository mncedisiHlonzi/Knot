//! Configuration for the Knot media worker.
//!
//! Every value comes from the environment, and every variable is the one the Go
//! API already reads (`KNOT_POSTGRES_DSN`, `KNOT_REDIS_ADDR`, `KNOT_S3_*`), so a
//! deployment adds a process rather than a second set of secrets. The worker reads
//! nothing the API does not already hold: it needs the queue, the bucket, and the
//! database, and nothing else (KNOT-038).

use std::time::Duration;

use anyhow::{Context, Result};

/// How the worker behaves. Durations are already parsed, so the loop never does
/// arithmetic on strings.
#[derive(Debug, Clone)]
pub struct Config {
    /// Redis connection string, e.g. `redis://127.0.0.1:6379/0`.
    pub redis_url: String,
    /// PostgreSQL DSN, shared with the API.
    pub database_url: String,
    /// Bucket the uploads and derivatives live in.
    pub bucket: String,
    /// S3-compatible endpoint. `None` means AWS's own endpoint.
    pub endpoint: Option<String>,
    /// Bucket region. R2 wants `auto`.
    pub region: String,
    /// Access key id for the bucket.
    pub access_key: String,
    /// Secret access key for the bucket.
    pub secret_key: String,
    /// How many jobs run at once. Encoding is CPU-bound, so this is deliberately
    /// small by default: one job already saturates a core, and the point of a
    /// dedicated worker is predictable memory, not maximum parallelism.
    pub concurrency: usize,
    /// How long one `BLMOVE` waits before returning empty and re-checking.
    pub claim_timeout: Duration,
    /// How often the reclaimer looks for jobs a dead worker left behind.
    pub reclaim_interval: Duration,
    /// How long a claimed job may sit in `processing` before it is considered
    /// abandoned.
    pub stale_after: Duration,
    /// Attempts before a job is moved to the failed list and its row is marked
    /// failed.
    pub max_attempts: u32,
}

impl Config {
    /// Reads the configuration from the environment.
    ///
    /// It fails rather than defaulting a credential: a worker that starts with a
    /// guessed bucket would accept jobs it cannot finish, and every upload would
    /// be marked failed instead of being retried once an operator fixes the
    /// deployment.
    pub fn from_env() -> Result<Self> {
        Ok(Self {
            redis_url: redis_url()?,
            database_url: required("KNOT_POSTGRES_DSN")?,
            bucket: required("KNOT_S3_BUCKET")?,
            endpoint: non_empty("KNOT_S3_ENDPOINT"),
            region: non_empty("KNOT_S3_REGION").unwrap_or_else(|| "auto".to_string()),
            access_key: required("KNOT_S3_ACCESS_KEY")?,
            secret_key: required("KNOT_S3_SECRET_KEY")?,
            concurrency: number("KNOT_MEDIA_WORKER_CONCURRENCY", 2)?,
            claim_timeout: Duration::from_secs(number("KNOT_MEDIA_CLAIM_TIMEOUT_SECS", 5)? as u64),
            reclaim_interval: Duration::from_secs(number(
                "KNOT_MEDIA_RECLAIM_INTERVAL_SECS",
                60,
            )? as u64),
            stale_after: Duration::from_secs(number("KNOT_MEDIA_STALE_SECS", 300)? as u64),
            max_attempts: number("KNOT_MEDIA_MAX_ATTEMPTS", 3)?,
        })
    }
}

/// Builds the Redis URL from the variables the API uses.
///
/// `KNOT_REDIS_URL` wins when set, because a managed Redis usually hands out a
/// complete URL and splitting it back into parts would only lose information.
fn redis_url() -> Result<String> {
    if let Some(url) = non_empty("KNOT_REDIS_URL") {
        return Ok(url);
    }

    let addr = non_empty("KNOT_REDIS_ADDR").unwrap_or_else(|| "127.0.0.1:6379".to_string());
    let db = number::<u32>("KNOT_REDIS_DB", 0)?;

    Ok(match non_empty("KNOT_REDIS_PASSWORD") {
        Some(password) => format!("redis://:{password}@{addr}/{db}"),
        None => format!("redis://{addr}/{db}"),
    })
}

/// Returns a variable's value, or an error naming it.
fn required(key: &str) -> Result<String> {
    non_empty(key).with_context(|| format!("{key} must be set"))
}

/// Returns a variable's value, or `None` when it is unset or blank.
fn non_empty(key: &str) -> Option<String> {
    std::env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

/// Parses a numeric variable, falling back to `default`.
fn number<T>(key: &str, default: T) -> Result<T>
where
    T: std::str::FromStr,
    T::Err: std::fmt::Display,
{
    match non_empty(key) {
        None => Ok(default),
        Some(raw) => raw
            .parse()
            .map_err(|error| anyhow::anyhow!("{key} is not a number ({raw}): {error}")),
    }
}
