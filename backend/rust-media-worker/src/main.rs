//! Knot's media processing worker.
//!
//! The Go API no longer encodes anything. It stores one upload, inserts the row
//! as `processing`, and appends a job to a Redis list; this process is what turns
//! that job into the derivatives a reader is actually served. The split exists
//! because encoding is CPU- and memory-bound in a way a request-serving Go process
//! is bad at (garbage collection, CGo, unpredictable peak memory), while it is
//! exactly what a small dedicated Rust binary is good at (KNOT-038).
//!
//! The shape of the process is: N worker loops that each block on `BLMOVE` for one
//! job at a time, and one reclaimer that returns jobs a dead worker left behind.
//! Every durable step — the claim, the completion, the failure — is a Redis
//! operation on a list, so a crash loses no work and duplicates none.

mod config;
mod db;
mod images;
mod job;
mod keys;
mod queue;
mod storage;
#[cfg(test)]
mod test_support;
mod video;
mod worker;

use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use anyhow::Result;
use tracing::{error, info, warn};
use tracing_subscriber::EnvFilter;

use crate::config::Config;
use crate::db::{MediaRows, PgMediaRows};
use crate::queue::{Failure, Queue, RedisQueue};
use crate::storage::S3ObjectStore;

fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let config = Config::from_env()?;

    info!(
        concurrency = config.concurrency,
        queue = keys::JOBS,
        bucket = %config.bucket,
        "starting the Knot media worker"
    );

    if !video::ffmpeg_is_available() {
        warn!("ffmpeg is not on PATH: every video job will fail until it is installed");
    }

    let store = Arc::new(S3ObjectStore::from_config(&config).await?);
    let rows = Arc::new(PgMediaRows::connect(&config.database_url).await?);

    let reclaimer = tokio::spawn(reclaim_loop(
        Queue::new(RedisQueue::connect(&config.redis_url).await?, config.max_attempts),
        rows.clone(),
        config.clone(),
    ));

    let mut workers = Vec::with_capacity(config.concurrency);
    for index in 0..config.concurrency {
        let queue = Queue::new(
            RedisQueue::connect(&config.redis_url).await?,
            config.max_attempts,
        );
        workers.push(tokio::spawn(worker_loop(
            index,
            queue,
            store.clone(),
            rows.clone(),
            config.clone(),
        )));
    }

    // The process runs until it is told to stop. A job interrupted here is NOT
    // lost: its payload is still in the processing list, so the next start's
    // reclaimer returns it once the claim goes stale. That is the whole reason a
    // claim is a `BLMOVE` rather than a `LPOP`.
    tokio::signal::ctrl_c().await?;
    info!("shutting down");
    for worker in &workers {
        worker.abort();
    }
    reclaimer.abort();

    Ok(())
}

/// Blocks for one job at a time, forever.
async fn worker_loop(
    index: usize,
    mut queue: Queue<RedisQueue>,
    store: Arc<S3ObjectStore>,
    rows: Arc<PgMediaRows>,
    config: Config,
) {
    loop {
        let claimed = match queue.claim(config.claim_timeout, now()).await {
            Ok(claimed) => claimed,
            Err(error) => {
                // Redis being briefly unreachable is not a reason to exit: the
                // job list survives, so backing off is the whole recovery.
                warn!(worker = index, error = %format!("{error:#}"), "could not claim a job");
                tokio::time::sleep(Duration::from_secs(1)).await;
                continue;
            }
        };

        let Some(claim) = claimed else {
            continue;
        };

        info!(
            worker = index,
            media_id = %claim.job.media_id,
            media_type = ?claim.job.media_type,
            object_key = %claim.job.object_key,
            "claimed a media job"
        );

        match worker::process(&claim.job, store.as_ref(), rows.as_ref()).await {
            Ok(outcome) => {
                if let Err(error) = queue.complete(&claim).await {
                    warn!(
                        worker = index,
                        media_id = %claim.job.media_id,
                        error = %format!("{error:#}"),
                        "could not record the completion"
                    );
                }
                info!(
                    worker = index,
                    media_id = %claim.job.media_id,
                    abandoned = outcome.abandoned,
                    storage_key = %outcome.storage_key,
                    "finished a media job"
                );
            }
            Err(error) => {
                error!(
                    worker = index,
                    media_id = %claim.job.media_id,
                    media_type = ?claim.job.media_type,
                    error = %format!("{error:#}"),
                    "media job failed"
                );

                match queue.fail(&claim).await {
                    Ok(Failure::Retried { attempts }) => {
                        info!(
                            worker = index,
                            media_id = %claim.job.media_id,
                            attempts,
                            max_attempts = queue.max_attempts(),
                            "requeued the media job"
                        );
                    }
                    Ok(Failure::Exhausted { attempts }) => {
                        warn!(
                            worker = index,
                            media_id = %claim.job.media_id,
                            attempts,
                            "the media job exhausted its attempts"
                        );
                        if let Err(mark_error) = rows.mark_failed(&claim.job.media_id).await {
                            error!(
                                media_id = %claim.job.media_id,
                                error = %format!("{mark_error:#}"),
                                "could not mark the media row failed"
                            );
                        }
                    }
                    Err(requeue_error) => {
                        // The payload is still in the processing list, so the
                        // reclaimer will pick it up once the claim goes stale.
                        warn!(
                            worker = index,
                            media_id = %claim.job.media_id,
                            error = %format!("{requeue_error:#}"),
                            "could not record the failure"
                        );
                    }
                }
            }
        }
    }
}

/// Returns jobs whose worker stopped to the waiting line, forever.
async fn reclaim_loop(mut queue: Queue<RedisQueue>, rows: Arc<PgMediaRows>, config: Config) {
    let mut ticker = tokio::time::interval(config.reclaim_interval);
    // The first tick fires immediately, which is deliberate: a worker that starts
    // after a crash reclaims the abandoned work at once rather than a minute later.
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);

    loop {
        ticker.tick().await;

        match queue.reclaim_stale(config.stale_after, now()).await {
            Ok(report) if report.moved == 0 => {}
            Ok(report) => {
                info!(
                    moved = report.moved,
                    failed = report.failed.len(),
                    "reclaimed abandoned media jobs"
                );
                for job in report.failed {
                    if let Err(error) = rows.mark_failed(&job.media_id).await {
                        error!(
                            media_id = %job.media_id,
                            error = %format!("{error:#}"),
                            "could not mark a reclaimed media row failed"
                        );
                    }
                }
            }
            Err(error) => {
                warn!(error = %format!("{error:#}"), "could not reclaim abandoned jobs");
            }
        }
    }
}
