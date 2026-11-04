//! Loading the repository's `.env` files, the way the Go API does.
//!
//! The worker is normally started from `backend/rust-media-worker`, one level
//! deeper than the Go API's `backend/go`, so the repository root is still two
//! directories up. Two files are read, in this order (KNOT-038-fix):
//!
//! 1. `.env.example` — committed, shared defaults (Postgres port, Redis address,
//!    S3 bucket name). It must NOT override anything already set, so a real
//!    environment variable exported by the shell or set by the host wins.
//! 2. `.env` — gitignored, real secrets. It OVERRIDES the example, because those
//!    are the values that actually work: the example's credentials are
//!    placeholders, and a placeholder that shadowed a real key would be worse
//!    than no default at all.
//!
//! Nothing here reads a value into the log. A file that cannot be read is a
//! warning, not a failure: a deployment sets real environment variables and has
//! no files at all, and that must keep working.

use std::fs;
use std::path::{Path, PathBuf};

/// The committed defaults, relative to `backend/rust-media-worker`.
pub const ENV_EXAMPLE: &str = "../../.env.example";

/// The gitignored secrets, relative to `backend/rust-media-worker`.
pub const ENV_LOCAL: &str = "../../.env";

/// The keys the worker cannot run without, or reads with a documented default.
///
/// `KNOT_REDIS_ADDR` is in the list even though it has a default, because an
/// operator who thinks they are pointing at a managed Redis deserves to see
/// whether the variable was actually read.
pub const REPORTED_KEYS: [&str; 6] = [
    "KNOT_POSTGRES_DSN",
    "KNOT_REDIS_ADDR",
    "KNOT_S3_ENDPOINT",
    "KNOT_S3_BUCKET",
    "KNOT_S3_ACCESS_KEY",
    "KNOT_S3_SECRET_KEY",
];

/// What loading the files did, so `main` can log it once tracing exists.
///
/// The warnings are collected rather than printed: the files are read before the
/// logger is configured (so `RUST_LOG` in `.env` can take effect), and a warning
/// printed before that would be written to nowhere.
#[derive(Debug, Default)]
pub struct LoadedEnv {
    /// The files that were read, in load order.
    pub files: Vec<PathBuf>,
    /// The files that could not be read, and why.
    pub warnings: Vec<String>,
    /// How many lines were skipped because their value would not parse.
    pub skipped_lines: usize,
}

/// Loads `.env.example` and then `.env`, with `.env` overriding the example.
///
/// **The files are parsed one line at a time, deliberately.** `dotenvy` — like
/// `godotenv` before it — rejects an unquoted value containing a space, and its
/// file-level API stops at the FIRST such line. Both of this repository's files
/// contain one (`KNOT_EMAIL_FROM=Knot <onboarding@resend.dev>`), and in `.env` it
/// sits at line 6, before every R2 setting: a file-level load therefore leaves the
/// worker starting "successfully" against an incomplete configuration (KNOT-038-fix).
/// Feeding dotenvy one line at a time confines a bad line to itself, which is what
/// `godotenv` effectively does for Go.
///
/// A missing or unreadable file is a warning, not a failure: a deployment sets real
/// environment variables and has no files at all.
pub fn load() -> LoadedEnv {
    let mut loaded = LoadedEnv::default();

    // The committed defaults must not clobber a variable the host already set, so
    // they are applied without overriding.
    load_file(Path::new(ENV_EXAMPLE), false, &mut loaded);
    // The secrets ARE the values that work; they override the example's
    // placeholders (and a shell value, exactly as the brief asks).
    load_file(Path::new(ENV_LOCAL), true, &mut loaded);

    loaded
}

/// Applies one file line by line, recording anything that had to be skipped.
///
/// The skip message names the FILE, the LINE NUMBER and the KEY — never the value
/// and never dotenvy's own error text, because that error quotes the offending
/// line. A malformed secret must not be copied into a log stream.
fn load_file(path: &Path, override_existing: bool, loaded: &mut LoadedEnv) {
    let contents = match fs::read_to_string(path) {
        Ok(contents) => contents,
        Err(error) => {
            loaded.warnings.push(format!(
                "could not read {}: {error} (run the worker from backend/rust-media-worker, or set the variables in the environment)",
                path.display()
            ));
            return;
        }
    };

    loaded.files.push(path.to_path_buf());

    for (index, line) in contents.lines().enumerate() {
        let applied = if override_existing {
            dotenvy::from_read_override(line.as_bytes())
        } else {
            dotenvy::from_read(line.as_bytes())
        };

        if applied.is_err() {
            loaded.skipped_lines += 1;
            loaded.warnings.push(format!(
                "skipped {}:{} ({}): the value would not parse unquoted",
                path.display(),
                index + 1,
                key_of(line)
            ));
        }
    }
}

/// Returns the variable name of a `KEY=value` line, for a message that says which
/// line was skipped without quoting it.
fn key_of(line: &str) -> &str {
    match line.split_once('=') {
        Some((key, _)) if !key.trim().is_empty() => key.trim(),
        _ => "an unrecognised line",
    }
}

/// Returns the names of [`REPORTED_KEYS`] that are set to a non-blank value.
///
/// Only names come back. Values are never returned by this function, so a later
/// change cannot log one by accident.
pub fn resolved_keys() -> (Vec<&'static str>, Vec<&'static str>) {
    let mut resolved = Vec::with_capacity(REPORTED_KEYS.len());
    let mut missing = Vec::new();

    for key in REPORTED_KEYS {
        let set = std::env::var(key)
            .map(|value| !value.trim().is_empty())
            .unwrap_or(false);
        if set {
            resolved.push(key);
        } else {
            missing.push(key);
        }
    }

    (resolved, missing)
}
