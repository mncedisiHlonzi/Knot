//! The one database write the worker needs: finish a row.
//!
//! The worker does not own the schema and does not read rows: the Go API inserted
//! the row as `processing`, and all that is left is to record the derivatives it
//! now has. Keeping the write this small is what makes it safe for the worker to
//! be a separate process with its own connection pool (KNOT-038).

use anyhow::{Context, Result};
use async_trait::async_trait;
use sqlx::postgres::PgPoolOptions;
use sqlx::PgPool;

/// The row update the pipeline performs.
#[async_trait]
pub trait MediaRows: Send + Sync {
    /// Marks a row ready with the derivatives it now serves.
    ///
    /// Returns `false` when no row matched, which means the item was deleted
    /// while its job was queued or running. The worker treats that as "abandon":
    /// there is nothing left to serve, so it removes the objects it just wrote.
    ///
    /// `width` and `height` are the decoded dimensions, and they are only used
    /// when the row has none: a client that reported dimensions keeps them, and a
    /// row that has none learns the true shape of its media.
    ///
    /// `poster_key` is the WebP still written for a video, and `None` for an
    /// image: an image has its own picture, so a poster would be a second copy of
    /// it (KNOT-042).
    async fn mark_ready(
        &self,
        media_id: &str,
        storage_key: &str,
        thumbnail_key: Option<&str>,
        poster_key: Option<&str>,
        size_bytes: i64,
        width: Option<i32>,
        height: Option<i32>,
    ) -> Result<bool>;

    /// Marks a row failed, after the job exhausted its attempts.
    async fn mark_failed(&self, media_id: &str) -> Result<()>;
}

/// The PostgreSQL implementation.
pub struct PgMediaRows {
    pool: PgPool,
}

impl PgMediaRows {
    /// Opens a small pool. It is deliberately smaller than the API's: the worker's
    /// writes are one statement per job, and a large pool would only compete with
    /// the API for the same server connections.
    pub async fn connect(database_url: &str) -> Result<Self> {
        let pool = PgPoolOptions::new()
            .max_connections(4)
            .connect(database_url)
            .await
            .context("connect to postgres")?;

        Ok(Self { pool })
    }
}

#[async_trait]
impl MediaRows for PgMediaRows {
    async fn mark_ready(
        &self,
        media_id: &str,
        storage_key: &str,
        thumbnail_key: Option<&str>,
        poster_key: Option<&str>,
        size_bytes: i64,
        width: Option<i32>,
        height: Option<i32>,
    ) -> Result<bool> {
        let result = sqlx::query(
            r#"
            UPDATE story_media
            SET status = 'ready',
                storage_key = $2,
                thumbnail_key = $3,
                poster_key = $4,
                size_bytes = $5,
                width = COALESCE(width, $6),
                height = COALESCE(height, $7)
            WHERE id = $1::uuid
            "#,
        )
        .bind(media_id)
        .bind(storage_key)
        .bind(thumbnail_key)
        .bind(poster_key)
        .bind(size_bytes)
        .bind(width)
        .bind(height)
        .execute(&self.pool)
        .await
        .with_context(|| format!("mark media {media_id} ready"))?;

        Ok(result.rows_affected() > 0)
    }

    async fn mark_failed(&self, media_id: &str) -> Result<()> {
        sqlx::query(
            r#"
            UPDATE story_media
            SET status = 'failed'
            WHERE id = $1::uuid
            "#,
        )
        .bind(media_id)
        .execute(&self.pool)
        .await
        .with_context(|| format!("mark media {media_id} failed"))?;

        Ok(())
    }
}
