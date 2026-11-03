//! The bucket operations the worker needs: download the upload, put a
//! derivative, delete the upload.
//!
//! They are behind [`ObjectStore`] for the same reason the queue is behind
//! [`crate::queue::QueueOps`]: the pipeline's rules should be testable without an
//! object store, and the S3 client is the least interesting part of them
//! (KNOT-038).

use anyhow::{Context, Result};
use async_trait::async_trait;
use aws_config::BehaviorVersion;
use aws_sdk_s3::config::{Credentials, Region};
use aws_sdk_s3::primitives::ByteStream;
use aws_sdk_s3::Client;

use crate::config::Config;

/// One object store.
#[async_trait]
pub trait ObjectStore: Send + Sync {
    /// Reads the whole object at `key`.
    async fn get(&self, key: &str) -> Result<Vec<u8>>;

    /// Writes `body` at `key` with `content_type`.
    async fn put(&self, key: &str, body: Vec<u8>, content_type: &str) -> Result<()>;

    /// Removes `key`. Removing an object that is already gone is not an error.
    async fn delete(&self, key: &str) -> Result<()>;
}

/// The S3-compatible client: Cloudflare R2 in production, MinIO locally.
pub struct S3ObjectStore {
    client: Client,
    bucket: String,
}

impl S3ObjectStore {
    /// Builds a client from the configuration.
    ///
    /// A custom endpoint is forced to path-style addressing: R2 and MinIO both
    /// accept it, and virtual-host addressing against a custom endpoint resolves
    /// `<bucket>.<endpoint>`, which is not what a private bucket wants.
    pub async fn from_config(config: &Config) -> Result<Self> {
        let credentials = Credentials::new(
            &config.access_key,
            &config.secret_key,
            None,
            None,
            "knot-media-worker",
        );

        let shared = aws_config::defaults(BehaviorVersion::latest())
            .credentials_provider(credentials)
            .region(Region::new(config.region.clone()))
            .load()
            .await;

        let mut builder = aws_sdk_s3::config::Builder::from(&shared);
        if let Some(endpoint) = &config.endpoint {
            builder = builder.endpoint_url(endpoint).force_path_style(true);
        }

        Ok(Self {
            client: Client::from_conf(builder.build()),
            bucket: config.bucket.clone(),
        })
    }
}

#[async_trait]
impl ObjectStore for S3ObjectStore {
    async fn get(&self, key: &str) -> Result<Vec<u8>> {
        let output = self
            .client
            .get_object()
            .bucket(&self.bucket)
            .key(key)
            .send()
            .await
            .with_context(|| format!("download {key}"))?;

        let body = output
            .body
            .collect()
            .await
            .with_context(|| format!("read {key}"))?;

        Ok(body.into_bytes().to_vec())
    }

    async fn put(&self, key: &str, body: Vec<u8>, content_type: &str) -> Result<()> {
        self.client
            .put_object()
            .bucket(&self.bucket)
            .key(key)
            .content_type(content_type)
            .body(ByteStream::from(body))
            .send()
            .await
            .with_context(|| format!("upload {key}"))?;

        Ok(())
    }

    async fn delete(&self, key: &str) -> Result<()> {
        self.client
            .delete_object()
            .bucket(&self.bucket)
            .key(key)
            .send()
            .await
            .with_context(|| format!("delete {key}"))?;

        Ok(())
    }
}
