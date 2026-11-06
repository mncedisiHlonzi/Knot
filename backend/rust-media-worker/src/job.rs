//! The job payload and the object keys derived from it.
//!
//! The payload is deliberately minimal: the row to finish, the story it belongs
//! to, the uploaded object, and which pipeline to run. Every derivative key is
//! derived from the upload's key, so the two processes agree on the naming scheme
//! without sharing code. Deriving keys is also the only place a key is invented,
//! which is what keeps a worker from writing outside the story's prefix
//! (KNOT-038).

use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};

use crate::keys::{
    AVIF_EXTENSION, DISPLAY_SUFFIX, LQIP_SUFFIX, MP4_EXTENSION, ORIGINAL_SUFFIX, POSTER_SUFFIX,
    STORY_MEDIA_NAMESPACE, STREAM_SUFFIX, THUMBNAIL_SUFFIX, WEBP_EXTENSION,
};

/// Which pipeline a job runs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum MediaType {
    /// A still image: an AVIF display rendition and a WebP thumbnail.
    Image,
    /// A video: one H.264 stream rendition.
    Video,
}

/// One unit of media work, exactly as the Go API writes it.
///
/// The field names are the JSON keys the API marshals (`internal/storymedia/jobs.go`
/// `Job`), so a rename on either side is a contract break rather than a silent
/// mismatch: an unknown shape fails to deserialize and the job is moved to the
/// failed list instead of being processed against the wrong row.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Job {
    /// The `story_media` row to complete.
    pub media_id: String,
    /// The story the row belongs to.
    pub story_id: String,
    /// The uploaded file in the bucket.
    pub object_key: String,
    /// Which pipeline to run.
    pub media_type: MediaType,
}

impl Job {
    /// Parses one queued payload.
    pub fn parse(raw: &str) -> Result<Self> {
        let job: Job = serde_json::from_str(raw).context("parse media job")?;
        job.validate()?;
        Ok(job)
    }

    /// Rejects a payload whose ids or key would make the worker write somewhere it
    /// should not.
    ///
    /// The key check is not cosmetic: the worker derives every derivative key from
    /// `object_key`, so a forged or corrupted payload could otherwise place objects
    /// under another story's prefix.
    pub fn validate(&self) -> Result<()> {
        if self.media_id.trim().is_empty() {
            bail!("media_id is empty");
        }
        if self.story_id.trim().is_empty() {
            bail!("story_id is empty");
        }
        // Deriving the base is the check: it refuses a key outside this story.
        self.base_key()?;
        Ok(())
    }

    /// The object-key stem every derivative of this upload is named from:
    /// `story-media/<story id>/<uuid>`.
    pub fn base_key(&self) -> Result<String> {
        let prefix = format!("{STORY_MEDIA_NAMESPACE}{}/", self.story_id);
        if !self.object_key.starts_with(&prefix) {
            bail!(
                "object key {:?} is not under {:?}",
                self.object_key,
                prefix
            );
        }

        let position = self
            .object_key
            .rfind(ORIGINAL_SUFFIX)
            .with_context(|| format!("object key {:?} has no {ORIGINAL_SUFFIX}", self.object_key))?;
        if position < prefix.len() {
            bail!(
                "object key {:?} names no file beside {:?}",
                self.object_key,
                prefix
            );
        }

        // The suffix must be followed by an extension or the end of the key, so
        // "_originalish.png" is not mistaken for an original.
        let rest = &self.object_key[position + ORIGINAL_SUFFIX.len()..];
        if !(rest.is_empty() || rest.starts_with('.')) {
            bail!("object key {:?} has no extension after the suffix", self.object_key);
        }

        Ok(self.object_key[..position].to_string())
    }

    /// The display AVIF's key: the image's full rendition.
    pub fn display_key(&self) -> Result<String> {
        Ok(format!(
            "{}{DISPLAY_SUFFIX}{AVIF_EXTENSION}",
            self.base_key()?
        ))
    }

    /// The display WebP's key: the same rendition in the one codec every device
    /// can decode (KNOT-038-fix3).
    pub fn display_webp_key(&self) -> Result<String> {
        Ok(format!(
            "{}{DISPLAY_SUFFIX}{WEBP_EXTENSION}",
            self.base_key()?
        ))
    }

    /// The thumbnail's key: the 400px WebP the feed card loads.
    pub fn thumbnail_key(&self) -> Result<String> {
        Ok(format!(
            "{}{THUMBNAIL_SUFFIX}{WEBP_EXTENSION}",
            self.base_key()?
        ))
    }

    /// The stream MP4's key: the video's full rendition.
    pub fn stream_key(&self) -> Result<String> {
        Ok(format!("{}{STREAM_SUFFIX}{MP4_EXTENSION}", self.base_key()?))
    }

    /// The poster's key: the WebP still a card draws before the first frame.
    ///
    /// A video's poster is written beside its stream, so the two files always
    /// describe the same upload and deleting one row can find both from the key
    /// alone (KNOT-042).
    pub fn poster_key(&self) -> Result<String> {
        Ok(format!("{}{POSTER_SUFFIX}{WEBP_EXTENSION}", self.base_key()?))
    }

    /// The placeholder's key: the ~20px WebP a client draws before anything
    /// sharper exists.
    ///
    /// Both media types write it, from the same base name, so the API can derive
    /// it from either the thumbnail or the poster key without storing a fourth
    /// column (KNOT-043).
    pub fn lqip_key(&self) -> Result<String> {
        Ok(format!("{}{LQIP_SUFFIX}{WEBP_EXTENSION}", self.base_key()?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const STORY: &str = "55555555-5555-4555-8555-555555555555";
    const MEDIA: &str = "00000000-0000-4000-8000-000000000001";

    fn upload_key() -> String {
        format!("story-media/{STORY}/abc_original.png")
    }

    fn job(media_type: MediaType) -> Job {
        Job {
            media_id: MEDIA.to_string(),
            story_id: STORY.to_string(),
            object_key: upload_key(),
            media_type,
        }
    }

    #[test]
    fn parses_the_api_payload() {
        let raw = format!(
            r#"{{"media_id":"{MEDIA}","story_id":"{STORY}","object_key":"{}","media_type":"image"}}"#,
            upload_key()
        );

        let parsed = Job::parse(&raw).expect("a well-formed payload parses");

        assert_eq!(parsed.media_id, MEDIA);
        assert_eq!(parsed.story_id, STORY);
        assert_eq!(parsed.media_type, MediaType::Image);
    }

    #[test]
    fn parses_a_video_payload() {
        let raw = r#"{"media_id":"m","story_id":"s","object_key":"story-media/s/x_original.mp4","media_type":"video"}"#;

        // The ids are not validated as UUIDs here on purpose — that is the API's
        // job; the worker only needs them to be present and to agree with the key.
        let parsed = Job::parse(raw).expect("the payload parses");

        assert_eq!(parsed.media_type, MediaType::Video);
    }

    #[test]
    fn rejects_an_unknown_media_type() {
        let raw = format!(
            r#"{{"media_id":"{MEDIA}","story_id":"{STORY}","object_key":"{}","media_type":"audio"}}"#,
            upload_key()
        );

        assert!(Job::parse(&raw).is_err());
    }

    #[test]
    fn derives_every_derivative_beside_the_upload() {
        let job = job(MediaType::Image);

        assert_eq!(
            job.base_key().unwrap(),
            format!("story-media/{STORY}/abc")
        );
        assert_eq!(
            job.display_key().unwrap(),
            format!("story-media/{STORY}/abc_display.avif")
        );
        assert_eq!(
            job.display_webp_key().unwrap(),
            format!("story-media/{STORY}/abc_display.webp")
        );
        assert_eq!(
            job.thumbnail_key().unwrap(),
            format!("story-media/{STORY}/abc_thumb.webp")
        );
        assert_eq!(
            job.stream_key().unwrap(),
            format!("story-media/{STORY}/abc_stream.mp4")
        );
        // The poster is written beside the stream from the same base key, so the
        // two files cannot disagree about which upload they belong to (KNOT-042).
        assert_eq!(
            job.poster_key().unwrap(),
            format!("story-media/{STORY}/abc_poster.webp")
        );
        // And so is the placeholder (KNOT-043).
        assert_eq!(
            job.lqip_key().unwrap(),
            format!("story-media/{STORY}/abc_lqip.webp")
        );
    }

    #[test]
    fn derives_the_base_from_a_mov_upload() {
        let job = Job {
            media_id: MEDIA.to_string(),
            story_id: STORY.to_string(),
            object_key: format!("story-media/{STORY}/abc_original.mov"),
            media_type: MediaType::Video,
        };

        assert_eq!(
            job.stream_key().unwrap(),
            format!("story-media/{STORY}/abc_stream.mp4")
        );
    }

    #[test]
    fn rejects_a_key_outside_the_story_prefix() {
        let job = Job {
            media_id: MEDIA.to_string(),
            story_id: STORY.to_string(),
            object_key: "story-media/00000000-0000-4000-8000-999999999999/abc_original.png"
                .to_string(),
            media_type: MediaType::Image,
        };

        assert!(job.base_key().is_err());
    }

    #[test]
    fn rejects_a_key_without_the_original_suffix() {
        let job = Job {
            media_id: MEDIA.to_string(),
            story_id: STORY.to_string(),
            object_key: format!("story-media/{STORY}/abc.png"),
            media_type: MediaType::Image,
        };

        assert!(job.base_key().is_err());
    }

    #[test]
    fn rejects_a_suffix_that_is_not_a_separate_word() {
        let job = Job {
            media_id: MEDIA.to_string(),
            story_id: STORY.to_string(),
            object_key: format!("story-media/{STORY}/abc_originalish.png"),
            media_type: MediaType::Image,
        };

        assert!(job.base_key().is_err());
    }
}
