# Knot media worker

The CPU-bound half of Knot's media pipeline. The Go API stores an upload, inserts
a `story_media` row as `processing`, and appends a job to Redis; this binary is
what turns that job into the files a client is actually served (KNOT-038).

## What one job does

1. Claims a payload with `BLMOVE knot:media:jobs knot:media:processing LEFT LEFT`.
2. Downloads the upload from the bucket.
3. Produces the derivatives:
   - **image** — a 1200px AVIF display (quality 75) and a 400px WebP thumbnail
     (quality 80);
   - **video** — a 1080p H.264 + AAC MP4 with `-movflags +faststart`.
4. Uploads them beside the upload (`_display.avif`, `_thumb.webp`, `_stream.mp4`).
5. Marks the row `ready` with those keys, the served size, and the decoded
   dimensions when the row had none.
6. Deletes the upload, then `LREM`s the payload into `knot:media:completed`.

A failure counts an attempt (`knot:media:attempts:<media id>`) and returns the job
to the tail of the waiting line while attempts remain; after `KNOT_MEDIA_MAX_ATTEMPTS`
(default 3) the payload moves to `knot:media:failed` and the row is marked `failed`.
Anything the attempt had already uploaded is removed, so a retry never inherits half
an item.

A **reclaimer** runs every `KNOT_MEDIA_RECLAIM_INTERVAL_SECS` and returns jobs whose
claim is older than `KNOT_MEDIA_STALE_SECS` (default 5 minutes) to circulation —
that is how a crashed or OOM-killed worker's work is recovered. If the row was
deleted while its job was queued, the worker discards what it wrote instead of
recording it.

## Requirements

- **Rust** (stable) to build.
- **FFmpeg on `PATH`** for video jobs. Images do not need it.

## Configuration

Started from this directory, the worker reads the repository's files before it looks
at the environment: `../../.env.example` (committed defaults) and then `../../.env`
(secrets), with the local file overriding the example. That is why `cargo run
--release` works without exporting anything by hand; a deployment with no files at
all keeps working too, because a missing file is only a warning.

The worker reads the same variables as the API — `KNOT_POSTGRES_DSN`,
`KNOT_REDIS_ADDR`/`PASSWORD`/`DB` (or `KNOT_REDIS_URL`), and `KNOT_S3_*` — plus its
own tuning:

| Variable | Default | Meaning |
| --- | --- | --- |
| `KNOT_MEDIA_WORKER_CONCURRENCY` | `2` | jobs run at once |
| `KNOT_MEDIA_CLAIM_TIMEOUT_SECS` | `5` | how long one `BLMOVE` blocks |
| `KNOT_MEDIA_RECLAIM_INTERVAL_SECS` | `60` | how often abandoned jobs are swept |
| `KNOT_MEDIA_STALE_SECS` | `300` | when a claim counts as abandoned |
| `KNOT_MEDIA_MAX_ATTEMPTS` | `3` | tries before the row is failed |

## Running

```bash
cd backend/rust-media-worker
cargo run --release
```

`RUST_LOG` selects the verbosity (`info` by default):

```bash
RUST_LOG=knot_media_worker=debug cargo run --release
```

## Notes

- `modernimage` (named in the KNOT-038 brief) ships prebuilt static libraries and
  has no `darwin-x86_64` asset, so a plain `cargo build` cannot succeed on every
  developer machine. The same codecs are used through portable crates instead:
  `image` (ravif for AVIF) and `webp` (libwebp for lossy WebP).
- The Redis keys, the `_original`/`_display`/`_thumb`/`_stream` suffixes, and the
  payload field names are a contract shared with `internal/storymedia`. They are
  spelled out in `src/keys.rs` and `src/job.rs`, and pinned by tests, so a change
  on either side is visible rather than silent.
