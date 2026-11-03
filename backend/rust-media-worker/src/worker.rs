//! One job, end to end: download the upload, produce the derivatives, store them,
//! finish the row, and remove the upload.
//!
//! The order is what makes the outcome trustworthy. The row is marked ready
//! BEFORE the upload is deleted, so a reader never sees `ready` with nothing
//! behind it; and if the row has disappeared — the item was deleted while it was
//! queued — everything the job wrote is removed again, because there is nothing
//! left that could ever serve it (KNOT-038).

use anyhow::Result;
use tracing::{info, warn};

use crate::db::MediaRows;
use crate::images;
use crate::job::{Job, MediaType};
use crate::keys::{AVIF_CONTENT_TYPE, VIDEO_CONTENT_TYPE, WEBP_CONTENT_TYPE};
use crate::storage::ObjectStore;
use crate::video;

/// What one processed job produced.
#[derive(Debug, Clone)]
pub struct Outcome {
    /// The object a reader is served for the full rendition.
    pub storage_key: String,
    /// The feed thumbnail's key, absent for a video (it plays instead of showing a
    /// cover).
    pub thumbnail_key: Option<String>,
    /// The primary object's size, which is what `Content-Length` and Range
    /// arithmetic describe.
    pub size_bytes: i64,
    /// The pixel width of the primary rendition, when it is known.
    pub width: Option<i32>,
    /// The pixel height of the primary rendition, when it is known.
    pub height: Option<i32>,
    /// True when the row no longer exists, so the objects were discarded instead
    /// of recorded. The job still counts as done: nothing will ever serve it.
    pub abandoned: bool,
}

/// Runs one job.
pub async fn process(job: &Job, store: &dyn ObjectStore, rows: &dyn MediaRows) -> Result<Outcome> {
    let upload = store.get(&job.object_key).await?;
    info!(
        media_id = %job.media_id,
        object_key = %job.object_key,
        upload_bytes = upload.len(),
        "downloaded the upload"
    );

    let mut uploaded: Vec<String> = Vec::new();

    let outcome = match job.media_type {
        MediaType::Image => {
            let derivatives = images::image_derivatives(&upload)?;
            let display_key = job.display_key()?;
            let thumbnail_key = job.thumbnail_key()?;

            info!(
                media_id = %job.media_id,
                width = derivatives.width,
                height = derivatives.height,
                display_bytes = derivatives.display.len(),
                thumbnail_bytes = derivatives.thumbnail.len(),
                "encoded the image derivatives"
            );

            // Measured before the buffer is moved into the upload, so the row
            // describes the file that was written without a second download.
            let display_bytes = derivatives.display.len() as i64;

            put(
                store,
                &display_key,
                derivatives.display,
                AVIF_CONTENT_TYPE,
                &mut uploaded,
            )
            .await?;
            put(
                store,
                &thumbnail_key,
                derivatives.thumbnail,
                WEBP_CONTENT_TYPE,
                &mut uploaded,
            )
            .await?;

            Outcome {
                storage_key: display_key,
                thumbnail_key: Some(thumbnail_key),
                size_bytes: display_bytes,
                width: Some(derivatives.width as i32),
                height: Some(derivatives.height as i32),
                abandoned: false,
            }
        }
        MediaType::Video => {
            let stream = video::transcode_to_stream(&upload)?;
            let stream_key = job.stream_key()?;
            let size_bytes = stream.len() as i64;

            info!(
                media_id = %job.media_id,
                stream_bytes = size_bytes,
                "transcoded the stream rendition"
            );

            put(store, &stream_key, stream, VIDEO_CONTENT_TYPE, &mut uploaded).await?;

            Outcome {
                storage_key: stream_key,
                thumbnail_key: None,
                size_bytes,
                // A video's frame size is FFmpeg's business until someone asks;
                // the row keeps whatever the uploader reported rather than a
                // guess.
                width: None,
                height: None,
                abandoned: false,
            }
        }
    };

    // The size is the one measured while the primary object was written: the row
    // describes the derivative it now serves, not the upload it replaced.
    let size_bytes = outcome.size_bytes;

    let ready = match rows
        .mark_ready(
            &job.media_id,
            &outcome.storage_key,
            outcome.thumbnail_key.as_deref(),
            size_bytes,
            outcome.width,
            outcome.height,
        )
        .await
    {
        Ok(ready) => ready,
        Err(error) => {
            discard(store, &uploaded).await;
            return Err(error);
        }
    };

    if !ready {
        warn!(
            media_id = %job.media_id,
            "the media row was deleted while its job ran; discarding the derivatives"
        );
        discard(store, &uploaded).await;
        discard(store, &[job.object_key.clone()]).await;

        return Ok(Outcome {
            abandoned: true,
            ..outcome
        });
    }

    // Deleting the upload is what keeps storage proportional to what a client
    // downloads. A failure here is an orphaned object, not a failed job: the row
    // is ready and the work is done.
    if let Err(error) = store.delete(&job.object_key).await {
        warn!(
            media_id = %job.media_id,
            object_key = %job.object_key,
            error = %error,
            "could not remove the upload"
        );
    }

    info!(
        media_id = %job.media_id,
        storage_key = %outcome.storage_key,
        thumbnail_key = outcome.thumbnail_key.as_deref().unwrap_or(""),
        size_bytes,
        "media is ready"
    );

    Ok(Outcome {
        size_bytes,
        ..outcome
    })
}

/// Writes one derivative, rolling back everything this job already wrote when the
/// write fails.
///
/// A partial set is worse than none: a row marked ready with a missing thumbnail
/// would show a broken card, and the next retry would have to guess which half
/// was written.
async fn put(
    store: &dyn ObjectStore,
    key: &str,
    body: Vec<u8>,
    content_type: &str,
    uploaded: &mut Vec<String>,
) -> Result<()> {
    if let Err(error) = store.put(key, body, content_type).await {
        warn!(key, error = %error, "derivative upload failed; discarding the ones already written");
        discard(store, uploaded).await;
        return Err(error);
    }

    uploaded.push(key.to_string());
    Ok(())
}

/// Removes objects, logging rather than failing: the caller is already handling a
/// failure or an abandoned job, and a second one must not hide the first.
async fn discard(store: &dyn ObjectStore, keys: &[String]) {
    for key in keys {
        if let Err(error) = store.delete(key).await {
            warn!(key, error = %error, "could not discard an object");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keys::{JOBS, STORY_MEDIA_NAMESPACE};
    use crate::test_support::{png, MEDIA_ID, STORY_ID};
    use async_trait::async_trait;
    use std::collections::HashMap;
    use std::sync::Mutex;

    #[derive(Default)]
    struct FakeStore {
        objects: Mutex<HashMap<String, Vec<u8>>>,
        deleted: Mutex<Vec<String>>,
        fail_put_on: Mutex<Option<String>>,
    }

    impl FakeStore {
        fn with_object(key: &str, body: Vec<u8>) -> Self {
            let store = FakeStore::default();
            store.objects.lock().unwrap().insert(key.to_string(), body);
            store
        }

        fn has(&self, key: &str) -> bool {
            self.objects.lock().unwrap().contains_key(key)
        }

        fn deleted(&self) -> Vec<String> {
            self.deleted.lock().unwrap().clone()
        }
    }

    #[async_trait]
    impl ObjectStore for FakeStore {
        async fn get(&self, key: &str) -> Result<Vec<u8>> {
            self.objects
                .lock()
                .unwrap()
                .get(key)
                .cloned()
                .ok_or_else(|| anyhow::anyhow!("missing {key}"))
        }

        async fn put(&self, key: &str, body: Vec<u8>, _content_type: &str) -> Result<()> {
            if self.fail_put_on.lock().unwrap().as_deref() == Some(key) {
                anyhow::bail!("simulated upload failure for {key}");
            }
            self.objects.lock().unwrap().insert(key.to_string(), body);
            Ok(())
        }

        async fn delete(&self, key: &str) -> Result<()> {
            self.deleted.lock().unwrap().push(key.to_string());
            self.objects.lock().unwrap().remove(key);
            Ok(())
        }
    }

    #[derive(Default)]
    struct FakeRows {
        exists: Mutex<bool>,
        ready: Mutex<Vec<ReadyCall>>,
        failed: Mutex<Vec<String>>,
        fail: Mutex<bool>,
    }

    #[derive(Debug, Clone)]
    struct ReadyCall {
        media_id: String,
        storage_key: String,
        thumbnail_key: Option<String>,
        size_bytes: i64,
        width: Option<i32>,
        height: Option<i32>,
    }

    impl FakeRows {
        fn existing() -> Self {
            FakeRows {
                exists: Mutex::new(true),
                ..FakeRows::default()
            }
        }

        fn ready_calls(&self) -> Vec<ReadyCall> {
            self.ready.lock().unwrap().clone()
        }
    }

    #[async_trait]
    impl MediaRows for FakeRows {
        async fn mark_ready(
            &self,
            media_id: &str,
            storage_key: &str,
            thumbnail_key: Option<&str>,
            size_bytes: i64,
            width: Option<i32>,
            height: Option<i32>,
        ) -> Result<bool> {
            if *self.fail.lock().unwrap() {
                anyhow::bail!("simulated database failure");
            }

            self.ready.lock().unwrap().push(ReadyCall {
                media_id: media_id.to_string(),
                storage_key: storage_key.to_string(),
                thumbnail_key: thumbnail_key.map(str::to_string),
                size_bytes,
                width,
                height,
            });

            Ok(*self.exists.lock().unwrap())
        }

        async fn mark_failed(&self, media_id: &str) -> Result<()> {
            self.failed.lock().unwrap().push(media_id.to_string());
            Ok(())
        }
    }

    fn image_job() -> (Job, String) {
        let key = format!("{STORY_MEDIA_NAMESPACE}{STORY_ID}/abc_original.png");
        (
            Job {
                media_id: MEDIA_ID.to_string(),
                story_id: STORY_ID.to_string(),
                object_key: key.clone(),
                media_type: MediaType::Image,
            },
            key,
        )
    }

    #[tokio::test]
    async fn an_image_produces_both_derivatives_and_removes_the_upload() {
        let (job, key) = image_job();
        let store = FakeStore::with_object(&key, png(320, 200));
        let rows = FakeRows::existing();

        let outcome = process(&job, &store, &rows).await.expect("the job succeeds");

        assert!(!outcome.abandoned);
        assert_eq!(outcome.storage_key, job.display_key().unwrap());
        assert_eq!(outcome.thumbnail_key, Some(job.thumbnail_key().unwrap()));
        assert!(store.has(&job.display_key().unwrap()));
        assert!(store.has(&job.thumbnail_key().unwrap()));
        assert!(!store.has(&key), "the upload is removed");
        assert_eq!(store.deleted(), vec![key]);

        let ready = rows.ready_calls();
        assert_eq!(ready.len(), 1);
        assert_eq!(ready[0].media_id, MEDIA_ID);
        assert_eq!(ready[0].thumbnail_key.as_deref(), Some(job.thumbnail_key().unwrap().as_str()));
        assert!(ready[0].size_bytes > 0, "the size describes the derivative");
        // The row learns the shape it never had, from the encoded picture.
        assert_eq!(ready[0].width, Some(320));
        assert_eq!(ready[0].height, Some(200));
    }

    #[tokio::test]

    async fn a_video_produces_one_stream_and_no_thumbnail() {
        if !video::ffmpeg_is_available() {
            eprintln!("skipping: ffmpeg is not on PATH");
            return;
        }

        let key = format!("{STORY_MEDIA_NAMESPACE}{STORY_ID}/abc_original.mp4");
        let store = FakeStore::with_object(&key, crate::test_support::mp4());
        let rows = FakeRows::existing();
        let job = Job {
            media_id: MEDIA_ID.to_string(),
            story_id: STORY_ID.to_string(),
            object_key: key.clone(),
            media_type: MediaType::Video,
        };

        let outcome = process(&job, &store, &rows).await.expect("the job succeeds");

        assert_eq!(outcome.storage_key, job.stream_key().unwrap());
        assert_eq!(outcome.thumbnail_key, None, "a video autoplays; it has no cover");
        assert!(store.has(&job.stream_key().unwrap()));
        assert!(!store.has(&key));
        assert_eq!(rows.ready_calls()[0].width, None);
    }

    #[tokio::test]
    async fn a_deleted_row_discards_everything_the_job_wrote() {
        let (job, key) = image_job();
        let store = FakeStore::with_object(&key, png(320, 200));
        let rows = FakeRows::default();
        *rows.exists.lock().unwrap() = false;

        let outcome = process(&job, &store, &rows).await.expect("the job finishes");

        assert!(outcome.abandoned);
        assert!(!store.has(&job.display_key().unwrap()));
        assert!(!store.has(&job.thumbnail_key().unwrap()));
        assert!(!store.has(&key));
    }

    #[tokio::test]
    async fn a_failed_second_upload_leaves_nothing_behind() {
        let (job, key) = image_job();
        let store = FakeStore::with_object(&key, png(320, 200));
        *store.fail_put_on.lock().unwrap() = Some(job.thumbnail_key().unwrap());
        let rows = FakeRows::existing();

        let result = process(&job, &store, &rows).await;

        assert!(result.is_err(), "the job fails so it can be retried");
        assert!(
            !store.has(&job.display_key().unwrap()),
            "the display written before the failure is removed"
        );
        assert!(store.has(&key), "the upload survives, so a retry can succeed");
        assert!(rows.ready_calls().is_empty());
    }

    #[tokio::test]
    async fn a_database_failure_discards_the_derivatives() {
        let (job, key) = image_job();
        let store = FakeStore::with_object(&key, png(320, 200));
        let rows = FakeRows::existing();
        *rows.fail.lock().unwrap() = true;

        let result = process(&job, &store, &rows).await;

        assert!(result.is_err());
        assert!(!store.has(&job.display_key().unwrap()));
        assert!(store.has(&key));
    }

    #[tokio::test]
    async fn an_upload_that_is_not_an_image_fails_before_writing() {
        let (job, key) = image_job();
        let store = FakeStore::with_object(&key, b"definitely not an image".to_vec());
        let rows = FakeRows::existing();

        assert!(process(&job, &store, &rows).await.is_err());
        assert!(store.deleted().is_empty(), "nothing was written to roll back");
        assert!(rows.ready_calls().is_empty());
    }

    #[test]
    fn the_waiting_line_is_the_one_the_api_writes() {
        // A pin, not a behaviour: the two processes would silently disagree if
        // either renamed the list.
        assert_eq!(JOBS, "knot:media:jobs");
    }
}
