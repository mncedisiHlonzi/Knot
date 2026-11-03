//! The reliable job queue.
//!
//! A plain `LPOP` loses a job the moment the process that popped it dies, so the
//! queue is built the reliable way: a claim is one atomic `BLMOVE` that moves the
//! payload from the waiting line to the processing line, a completion moves it on
//! to the completed line, and a failure either returns it to the waiting line or,
//! after enough attempts, moves it to the failed line. Nothing is ever deleted
//! before its outcome is known, and a job whose worker vanished is reclaimed
//! rather than lost (KNOT-038).
//!
//! The Redis interaction is behind [`QueueOps`] for one reason: the claim,
//! complete, fail and reclaim rules are the part worth testing, and they should be
//! testable without a server. [`FakeQueue`] is an in-memory list that behaves like
//! the real thing, and the tests at the bottom of this file drive the real
//! [`Queue`] through it.

#[cfg(test)]
use std::collections::{HashMap, VecDeque};
use std::time::Duration;

use anyhow::Result;
use async_trait::async_trait;
use tracing::{error, warn};

use crate::job::Job;
use crate::keys::{ATTEMPTS_PREFIX, CLAIMS, COMPLETED, FAILED, JOBS, PROCESSING};

/// The Redis operations the queue needs.
///
/// The set is deliberately the whole of what the algorithm uses: adding a method
/// here is how a queue behaviour gets added, and it keeps the in-memory fake
/// honest instead of letting it accumulate unrelated commands.
#[async_trait]
pub trait QueueOps: Send + Sync {
    /// Atomically pops the head of `source` and pushes it onto the head of
    /// `destination`, waiting up to `timeout_secs` for an item.
    async fn move_head(&mut self, source: &str, destination: &str, timeout_secs: u64)
        -> Result<Option<String>>;

    /// Removes one occurrence of `value` from the list at `key`.
    async fn remove(&mut self, key: &str, value: &str) -> Result<()>;

    /// Appends `value` to the tail of the list at `key`.
    async fn push_tail(&mut self, key: &str, value: &str) -> Result<()>;

    /// Returns every value of the list at `key`, head first, without removing any.
    async fn items(&mut self, key: &str) -> Result<Vec<String>>;

    /// Records when `media_id` was claimed.
    async fn set_claim(&mut self, media_id: &str, at: i64) -> Result<()>;

    /// Returns when `media_id` was claimed, or `None` if nothing recorded it.
    async fn claim_time(&mut self, media_id: &str) -> Result<Option<i64>>;

    /// Forgets a claim.
    async fn clear_claim(&mut self, media_id: &str) -> Result<()>;

    /// Increments and returns the attempt count for `media_id`.
    async fn bump_attempts(&mut self, media_id: &str) -> Result<u32>;

    /// Forgets the attempt count for `media_id`.
    async fn clear_attempts(&mut self, media_id: &str) -> Result<()>;
}

/// One job a worker has claimed.
#[derive(Debug, Clone)]
pub struct Claim {
    /// The exact payload that was moved into the processing line. It is kept
    /// verbatim so the acknowledgement removes the string that is really there,
    /// not a re-serialised version that might differ.
    pub raw: String,
    /// The parsed job.
    pub job: Job,
}

/// What a failure did to the job.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Failure {
    /// The job went back to the waiting line; the row stays `processing`.
    Retried {
        /// How many attempts have now been made.
        attempts: u32,
    },
    /// The job went to the failed line; the row must be marked failed.
    Exhausted {
        /// How many attempts were made in total.
        attempts: u32,
    },
}

/// What a reclaim pass did.
///
/// It carries the jobs that were given up on, not only a count: the caller has to
/// mark those rows failed, and a row left in `processing` for ever is exactly the
/// stuck state the reclaimer exists to prevent.
#[derive(Debug, Default, Clone)]
pub struct ReclaimReport {
    /// How many jobs were taken out of the processing line.
    pub moved: usize,
    /// The jobs that exhausted their attempts and were moved to the failed line.
    pub failed: Vec<Job>,
}

/// The queue, parameterised by its backend so the rules can be tested in memory.
pub struct Queue<B: QueueOps> {
    backend: B,
    max_attempts: u32,
}

impl<B: QueueOps> Queue<B> {
    /// Returns a queue that gives up on a job after `max_attempts`.
    ///
    /// An attempt count below one would mean "never retry" while still moving the
    /// job, so it is clamped: a job must be attempted at least once.
    pub fn new(backend: B, max_attempts: u32) -> Self {
        Self {
            backend,
            max_attempts: max_attempts.max(1),
        }
    }

    /// Claims one job, waiting up to `timeout` for one to arrive.
    ///
    /// A payload that does not parse is moved straight to the failed line: it can
    /// never succeed, and leaving it in the waiting line would make it the first
    /// thing every worker sees, forever.
    pub async fn claim(&mut self, timeout: Duration, now: i64) -> Result<Option<Claim>> {
        let timeout_secs = timeout.as_secs().max(1);
        let Some(raw) = self
            .backend
            .move_head(JOBS, PROCESSING, timeout_secs)
            .await?
        else {
            return Ok(None);
        };

        match Job::parse(&raw) {
            Ok(job) => {
                self.backend.set_claim(&job.media_id, now).await?;
                Ok(Some(Claim { raw, job }))
            }
            Err(error_) => {
                error!(payload = %raw, error = %error_, "discarding an unreadable media job");
                self.backend.remove(PROCESSING, &raw).await?;
                self.backend.push_tail(FAILED, &raw).await?;
                Ok(None)
            }
        }
    }

    /// Records a successful job.
    ///
    /// The claim is cleared and the attempt counter deleted, so a media id that is
    /// processed once leaves nothing behind in the queue's keyspace.
    pub async fn complete(&mut self, claim: &Claim) -> Result<()> {
        self.backend.remove(PROCESSING, &claim.raw).await?;
        self.backend.push_tail(COMPLETED, &claim.raw).await?;
        self.backend.clear_claim(&claim.job.media_id).await?;
        self.backend.clear_attempts(&claim.job.media_id).await?;
        Ok(())
    }

    /// Records a failed attempt, retrying while attempts remain.
    ///
    /// The payload goes back to the TAIL of the waiting line so one poison job
    /// cannot occupy the head and stall the queue behind it.
    pub async fn fail(&mut self, claim: &Claim) -> Result<Failure> {
        self.backend.remove(PROCESSING, &claim.raw).await?;
        self.backend.clear_claim(&claim.job.media_id).await?;

        let attempts = self.backend.bump_attempts(&claim.job.media_id).await?;
        if attempts < self.max_attempts {
            self.backend.push_tail(JOBS, &claim.raw).await?;
            return Ok(Failure::Retried { attempts });
        }

        self.backend.push_tail(FAILED, &claim.raw).await?;
        Ok(Failure::Exhausted { attempts })
    }

    /// Moves jobs stuck in the processing line back into circulation.
    ///
    /// A claim older than `stale_after` belongs to a worker that is no longer
    /// running — a crash, an OOM kill, a deploy — and nothing else will ever
    /// finish it. The report says how many jobs were moved and which of them were
    /// given up on.
    ///
    /// A claim with no timestamp cannot be shown to be fresh, so it is treated as
    /// abandoned: a missing claim reads as epoch 0, which is older than any real
    /// run. That is the recovery path for jobs queued by an older build.
    /// The reclaim also counts an attempt, so a job that kills its worker every
    /// time ends up in the failed list instead of looping forever.
    pub async fn reclaim_stale(
        &mut self,
        stale_after: Duration,
        now: i64,
    ) -> Result<ReclaimReport> {
        let stale_secs = stale_after.as_secs() as i64;
        let waiting = self.backend.items(PROCESSING).await?;
        let mut report = ReclaimReport::default();

        for raw in waiting {
            let job = match Job::parse(&raw) {
                Ok(job) => job,
                Err(error_) => {
                    // Unreadable: it can never complete, so it must not stay in a
                    // list that is only ever emptied by a successful worker.
                    warn!(payload = %raw, error = %error_, "reclaiming an unreadable media job");
                    self.backend.remove(PROCESSING, &raw).await?;
                    self.backend.push_tail(FAILED, &raw).await?;
                    report.moved += 1;
                    continue;
                }
            };

            let claimed_at = self.backend.claim_time(&job.media_id).await?.unwrap_or(0);
            if now.saturating_sub(claimed_at) < stale_secs {
                continue;
            }

            warn!(
                media_id = %job.media_id,
                claimed_at,
                "reclaiming a media job whose worker stopped"
            );

            self.backend.remove(PROCESSING, &raw).await?;
            self.backend.clear_claim(&job.media_id).await?;

            let attempts = self.backend.bump_attempts(&job.media_id).await?;
            if attempts < self.max_attempts {
                self.backend.push_tail(JOBS, &raw).await?;
            } else {
                self.backend.push_tail(FAILED, &raw).await?;
                report.failed.push(job);
            }
            report.moved += 1;
        }

        Ok(report)
    }

    /// The configured attempt limit.
    pub fn max_attempts(&self) -> u32 {
        self.max_attempts
    }
}

/// The Redis implementation.
///
/// Two connections, not one: `BLMOVE` blocks for as long as its timeout on the
/// connection it runs on, and a multiplexed connection that is blocked would stall
/// every other command sharing it. The blocking command therefore gets a
/// connection of its own.
pub struct RedisQueue {
    blocking: redis::aio::MultiplexedConnection,
    commands: redis::aio::MultiplexedConnection,
}

impl RedisQueue {
    /// Opens both connections.
    pub async fn connect(url: &str) -> Result<Self> {
        let client = redis::Client::open(url)?;
        let blocking = client.get_multiplexed_async_connection().await?;
        let commands = client.get_multiplexed_async_connection().await?;
        Ok(Self { blocking, commands })
    }
}

#[async_trait]
impl QueueOps for RedisQueue {
    async fn move_head(
        &mut self,
        source: &str,
        destination: &str,
        timeout_secs: u64,
    ) -> Result<Option<String>> {
        // LEFT LEFT: pop the head of the waiting line and push it onto the head of
        // the processing line. Because the producer appends at the tail, that is
        // first-in, first-out.
        let moved: Option<String> = redis::cmd("BLMOVE")
            .arg(source)
            .arg(destination)
            .arg("LEFT")
            .arg("LEFT")
            .arg(timeout_secs)
            .query_async(&mut self.blocking)
            .await?;

        Ok(moved)
    }

    async fn remove(&mut self, key: &str, value: &str) -> Result<()> {
        let _: i64 = redis::cmd("LREM")
            .arg(key)
            .arg(1)
            .arg(value)
            .query_async(&mut self.commands)
            .await?;

        Ok(())
    }

    async fn push_tail(&mut self, key: &str, value: &str) -> Result<()> {
        let _: i64 = redis::cmd("RPUSH")
            .arg(key)
            .arg(value)
            .query_async(&mut self.commands)
            .await?;

        Ok(())
    }

    async fn items(&mut self, key: &str) -> Result<Vec<String>> {
        let values: Vec<String> = redis::cmd("LRANGE")
            .arg(key)
            .arg(0)
            .arg(-1)
            .query_async(&mut self.commands)
            .await?;

        Ok(values)
    }

    async fn set_claim(&mut self, media_id: &str, at: i64) -> Result<()> {
        let _: i64 = redis::cmd("HSET")
            .arg(CLAIMS)
            .arg(media_id)
            .arg(at)
            .query_async(&mut self.commands)
            .await?;

        Ok(())
    }

    async fn claim_time(&mut self, media_id: &str) -> Result<Option<i64>> {
        let at: Option<i64> = redis::cmd("HGET")
            .arg(CLAIMS)
            .arg(media_id)
            .query_async(&mut self.commands)
            .await?;

        Ok(at)
    }

    async fn clear_claim(&mut self, media_id: &str) -> Result<()> {
        let _: i64 = redis::cmd("HDEL")
            .arg(CLAIMS)
            .arg(media_id)
            .query_async(&mut self.commands)
            .await?;

        Ok(())
    }

    async fn bump_attempts(&mut self, media_id: &str) -> Result<u32> {
        let key = format!("{ATTEMPTS_PREFIX}{media_id}");
        let attempts: u32 = redis::cmd("INCR").arg(key).query_async(&mut self.commands).await?;

        // An attempt counter that outlives the queue would slowly fill Redis with
        // one key per upload that ever failed once. A day is far longer than any
        // retry window and short enough to be self-cleaning.
        let _: i64 = redis::cmd("EXPIRE")
            .arg(format!("{ATTEMPTS_PREFIX}{media_id}"))
            .arg(86_400)
            .query_async(&mut self.commands)
            .await?;

        Ok(attempts)
    }

    async fn clear_attempts(&mut self, media_id: &str) -> Result<()> {
        let _: i64 = redis::cmd("DEL")
            .arg(format!("{ATTEMPTS_PREFIX}{media_id}"))
            .query_async(&mut self.commands)
            .await?;

        Ok(())
    }
}

/// An in-memory [`QueueOps`] that behaves like the Redis lists.
///
/// It exists so the claim/complete/fail/reclaim rules are exercised directly: a
/// test can control time, inspect every list, and simulate a worker that died by
/// claiming a job and then doing nothing. It is compiled only for tests, so the
/// release binary carries no fake queue.
#[cfg(test)]
#[derive(Default, Debug)]
pub struct FakeQueue {
    /// `knot:media:jobs`
    pub jobs: VecDeque<String>,
    /// `knot:media:processing`
    pub processing: VecDeque<String>,
    /// `knot:media:completed`
    pub completed: VecDeque<String>,
    /// `knot:media:failed`
    pub failed: VecDeque<String>,
    /// `knot:media:claimed_at`
    pub claims: HashMap<String, i64>,
    /// `knot:media:attempts:*`
    pub attempts: HashMap<String, u32>,
    /// When set, the next claim returns this instead of waiting. It is how a test
    /// simulates a payload the producer should never have written.
    pub next_raw: Option<String>,
    /// Every BLMOVE timeout the queue asked for, so a test can assert it blocks.
    pub timeouts: Vec<u64>,
}

#[cfg(test)]
impl FakeQueue {
    /// Returns a fake whose waiting line holds `payloads`, in order.
    pub fn with_jobs(payloads: &[String]) -> Self {
        Self {
            jobs: payloads.iter().cloned().collect(),
            ..Self::default()
        }
    }
}

#[cfg(test)]
#[async_trait]
impl QueueOps for FakeQueue {
    async fn move_head(
        &mut self,
        source: &str,
        destination: &str,
        timeout_secs: u64,
    ) -> Result<Option<String>> {
        self.timeouts.push(timeout_secs);

        if let Some(raw) = self.next_raw.take() {
            return Ok(Some(raw));
        }

        let source_list = match source {
            JOBS => &mut self.jobs,
            PROCESSING => &mut self.processing,
            other => anyhow::bail!("unexpected source {other}"),
        };
        let Some(value) = source_list.pop_front() else {
            return Ok(None);
        };

        match destination {
            PROCESSING => self.processing.push_front(value.clone()),
            other => anyhow::bail!("unexpected destination {other}"),
        }

        Ok(Some(value))
    }

    async fn remove(&mut self, key: &str, value: &str) -> Result<()> {
        let list = match key {
            JOBS => &mut self.jobs,
            PROCESSING => &mut self.processing,
            COMPLETED => &mut self.completed,
            FAILED => &mut self.failed,
            other => anyhow::bail!("unexpected key {other}"),
        };
        if let Some(position) = list.iter().position(|item| item == value) {
            list.remove(position);
        }

        Ok(())
    }

    async fn push_tail(&mut self, key: &str, value: &str) -> Result<()> {
        match key {
            JOBS => self.jobs.push_back(value.to_string()),
            COMPLETED => self.completed.push_back(value.to_string()),
            FAILED => self.failed.push_back(value.to_string()),
            other => anyhow::bail!("unexpected key {other}"),
        }

        Ok(())
    }

    async fn items(&mut self, key: &str) -> Result<Vec<String>> {
        match key {
            PROCESSING => Ok(self.processing.iter().cloned().collect()),
            other => anyhow::bail!("unexpected key {other}"),
        }
    }

    async fn set_claim(&mut self, media_id: &str, at: i64) -> Result<()> {
        self.claims.insert(media_id.to_string(), at);
        Ok(())
    }

    async fn claim_time(&mut self, media_id: &str) -> Result<Option<i64>> {
        Ok(self.claims.get(media_id).copied())
    }

    async fn clear_claim(&mut self, media_id: &str) -> Result<()> {
        self.claims.remove(media_id);
        Ok(())
    }

    async fn bump_attempts(&mut self, media_id: &str) -> Result<u32> {
        let entry = self.attempts.entry(media_id.to_string()).or_insert(0);
        *entry += 1;
        Ok(*entry)
    }

    async fn clear_attempts(&mut self, media_id: &str) -> Result<()> {
        self.attempts.remove(media_id);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::job::MediaType;

    const STORY: &str = "55555555-5555-4555-8555-555555555555";
    const MEDIA: &str = "00000000-0000-4000-8000-000000000001";

    fn payload(media_id: &str) -> String {
        format!(
            r#"{{"media_id":"{media_id}","story_id":"{STORY}","object_key":"story-media/{STORY}/{media_id}_original.png","media_type":"image"}}"#
        )
    }

    fn queue(payloads: &[String]) -> Queue<FakeQueue> {
        Queue::new(FakeQueue::with_jobs(payloads), 3)
    }

    #[tokio::test]
    async fn claim_moves_a_job_into_processing() {
        let mut queue = queue(&[payload(MEDIA)]);

        let claim = queue.claim(Duration::from_secs(5), 100).await.unwrap();

        let claim = claim.expect("a job was waiting");
        assert_eq!(claim.job.media_id, MEDIA);
        assert_eq!(claim.job.media_type, MediaType::Image);
        assert!(queue.backend.jobs.is_empty());
        assert_eq!(queue.backend.processing.len(), 1);
        assert_eq!(queue.backend.claims.get(MEDIA), Some(&100));
        assert_eq!(queue.backend.timeouts, vec![5]);
    }

    #[tokio::test]
    async fn claim_blocks_for_at_least_one_second() {
        let mut queue = queue(&[]);

        // A zero-length block would busy-loop the worker against Redis.
        assert!(queue
            .claim(Duration::ZERO, 0)
            .await
            .unwrap()
            .is_none());
        assert_eq!(queue.backend.timeouts, vec![1]);
    }

    #[tokio::test]
    async fn claim_is_first_in_first_out() {
        let second = "00000000-0000-4000-8000-000000000002";
        let mut queue = queue(&[payload(MEDIA), payload(second)]);

        let first = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();
        let next = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();

        assert_eq!(first.job.media_id, MEDIA);
        assert_eq!(next.job.media_id, second);
    }

    #[tokio::test]
    async fn complete_moves_the_job_to_completed_and_forgets_the_attempts() {
        let mut queue = queue(&[payload(MEDIA)]);
        let claim = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();
        queue.backend.attempts.insert(MEDIA.to_string(), 1);

        queue.complete(&claim).await.unwrap();

        assert!(queue.backend.processing.is_empty());
        assert_eq!(queue.backend.completed.len(), 1);
        assert_eq!(queue.backend.completed[0], claim.raw);
        assert!(!queue.backend.claims.contains_key(MEDIA));
        assert!(!queue.backend.attempts.contains_key(MEDIA));
    }

    #[tokio::test]
    async fn a_failure_below_the_limit_requeues_the_same_payload() {
        let mut queue = queue(&[payload(MEDIA)]);
        let claim = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();

        let outcome = queue.fail(&claim).await.unwrap();

        assert_eq!(outcome, Failure::Retried { attempts: 1 });
        assert!(queue.backend.processing.is_empty());
        assert_eq!(queue.backend.jobs.len(), 1);
        assert_eq!(queue.backend.jobs[0], claim.raw);
        assert!(!queue.backend.claims.contains_key(MEDIA));
    }

    #[tokio::test]
    async fn a_failure_at_the_limit_moves_the_job_to_failed() {
        let mut queue = queue(&[payload(MEDIA)]);
        // Two attempts have already been made, so this third one exhausts it.
        queue.backend.attempts.insert(MEDIA.to_string(), 2);
        let claim = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();

        let outcome = queue.fail(&claim).await.unwrap();

        assert_eq!(outcome, Failure::Exhausted { attempts: 3 });
        assert!(queue.backend.jobs.is_empty());
        assert_eq!(queue.backend.failed.len(), 1);
    }

    #[tokio::test]
    async fn a_requeued_job_waits_behind_the_jobs_already_queued() {
        let other = "00000000-0000-4000-8000-000000000002";
        let mut queue = queue(&[payload(MEDIA), payload(other)]);
        let first = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();

        // The first job fails, so it must wait behind the job that is still queued.
        queue.fail(&first).await.unwrap();
        let next = queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();

        assert_eq!(first.job.media_id, MEDIA);
        assert_eq!(next.job.media_id, other);
        assert_eq!(queue.backend.jobs.len(), 1);
        assert_eq!(queue.backend.jobs[0], first.raw);
    }

    #[tokio::test]
    async fn an_unreadable_payload_never_blocks_the_queue() {
        let mut queue = queue(&[]);
        queue.backend.next_raw = Some("{not json".to_string());

        let claim = queue.claim(Duration::from_secs(1), 0).await.unwrap();

        assert!(claim.is_none());
        assert_eq!(queue.backend.failed.len(), 1);
        assert!(queue.backend.processing.is_empty());
    }

    #[tokio::test]
    async fn reclaim_returns_a_stale_job_to_the_waiting_line() {
        let mut queue = queue(&[payload(MEDIA)]);
        let claim = queue.claim(Duration::from_secs(1), 1_000).await.unwrap().unwrap();

        // Six minutes after a claim, well past the five-minute threshold.
        let report = queue
            .reclaim_stale(Duration::from_secs(300), 1_360)
            .await
            .unwrap();

        assert_eq!(report.moved, 1);
        assert!(report.failed.is_empty());
        assert!(queue.backend.processing.is_empty());
        assert_eq!(queue.backend.jobs.len(), 1);
        assert_eq!(queue.backend.jobs[0], claim.raw);
        assert_eq!(queue.backend.attempts.get(MEDIA), Some(&1));
    }

    #[tokio::test]
    async fn reclaim_leaves_a_fresh_claim_alone() {
        let mut queue = queue(&[payload(MEDIA)]);
        queue.claim(Duration::from_secs(1), 1_000).await.unwrap().unwrap();

        let report = queue
            .reclaim_stale(Duration::from_secs(300), 1_120)
            .await
            .unwrap();

        assert_eq!(report.moved, 0);
        assert_eq!(queue.backend.processing.len(), 1);
        assert!(queue.backend.jobs.is_empty());
    }

    #[tokio::test]
    async fn reclaim_treats_a_claim_with_no_timestamp_as_abandoned() {
        let mut queue = queue(&[]);
        // A job left by a worker built before claims were recorded. The missing
        // timestamp reads as epoch 0, so any real "now" is long past the threshold.
        queue.backend.processing.push_back(payload(MEDIA));

        let report = queue
            .reclaim_stale(Duration::from_secs(300), 1_000_000)
            .await
            .unwrap();

        assert_eq!(report.moved, 1);
        assert_eq!(queue.backend.jobs.len(), 1);
    }

    #[tokio::test]
    async fn reclaim_fails_a_job_that_exhausted_its_attempts() {
        let mut queue = queue(&[payload(MEDIA)]);
        queue.claim(Duration::from_secs(1), 0).await.unwrap().unwrap();
        queue.backend.attempts.insert(MEDIA.to_string(), 2);

        let report = queue
            .reclaim_stale(Duration::from_secs(300), 10_000)
            .await
            .unwrap();

        assert_eq!(report.moved, 1);
        assert_eq!(report.failed.len(), 1, "the caller must mark its row failed");
        assert_eq!(report.failed[0].media_id, MEDIA);
        assert!(queue.backend.jobs.is_empty());
        assert_eq!(queue.backend.failed.len(), 1);
        assert!(!queue.backend.claims.contains_key(MEDIA));
    }

    #[tokio::test]
    async fn reclaim_moves_an_unreadable_payload_out_of_processing() {
        let mut queue = queue(&[]);
        queue.backend.processing.push_back("not a job".to_string());

        let report = queue
            .reclaim_stale(Duration::from_secs(300), 10)
            .await
            .unwrap();

        assert_eq!(report.moved, 1);
        assert!(report.failed.is_empty(), "an unreadable job names no row to fail");
        assert!(queue.backend.processing.is_empty());
        assert_eq!(queue.backend.failed.len(), 1);
    }

    #[tokio::test]
    async fn an_attempt_limit_below_one_is_clamped() {
        let queue = Queue::new(FakeQueue::default(), 0);

        assert_eq!(queue.max_attempts(), 1);
    }
}
