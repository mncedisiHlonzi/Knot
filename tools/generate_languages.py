#!/usr/bin/env python3
"""Regenerate the canonical language list from the ISO 639-3 reference table.

A language in Knot is an ISO 639-3 code: three lower-case letters, such as "eng"
or "zul". That list exists twice — once for the server
(`backend/go/internal/language/languages.go`) and once for the app
(`apps/mobile/src/data/languages.ts`) — because the app must offer a picker with
no request, and the server must validate with no dependency. The two copies are
identical, and a mobile test fails when they drift.

This script is what makes "regenerate from the source" a real instruction rather
than advice. It rewrites only the block between the BEGIN/END GENERATED markers in
each file, so the prose and the hand-written helpers around them are never touched,
and it is safe to run twice.

Usage, from the repository root:

    curl -sSL -o /tmp/iso-639-3.tab https://iso639-3.sil.org/sites/iso639-3/files/downloads/iso-639-3.tab
    python3 tools/generate_languages.py --source /tmp/iso-639-3.tab

--source is optional; without it the table is downloaded into a temp file.

The source is the SIL International ISO 639-3 code table (the registry of record
for ISO 639-3, https://iso639-3.sil.org/code_tables/download_tables). Only `Id`
and `Ref_Name` are kept: no scope, no language type, no ISO 639-2 aliases, because
Knot stores one canonical code and shows one English name. The names are the
registry's own reference names, verbatim — including "Pedi" for `nso` — so the
list is a faithful copy that can be diffed against a future revision of the table.
"""

from __future__ import annotations

import argparse
import csv
import pathlib
import sys
import tempfile
import urllib.request

SOURCE_URL = "https://iso639-3.sil.org/sites/iso639-3/files/downloads/iso-639-3.tab"

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent

GO_FILE = REPO_ROOT / "backend/go/internal/language/languages.go"
TS_FILE = REPO_ROOT / "apps/mobile/src/data/languages.ts"

GO_BEGIN = "\t// BEGIN GENERATED"
GO_END = "\t// END GENERATED"
TS_BEGIN = "  // BEGIN GENERATED"
TS_END = "  // END GENERATED"

# The floor the tests assert. ISO 639-3 has ~7,900 entries; anything far below
# that means the download was truncated, which is worth failing loudly for.
MINIMUM_ENTRIES = 7000


class Entry:
    """One canonical language: a 3-letter code and its reference name."""

    def __init__(self, code: str, name: str) -> None:
        self.code = code
        self.name = name


def download() -> pathlib.Path:
    """Fetches the ISO 639-3 table into a temporary file and returns its path."""
    handle, path = tempfile.mkstemp(prefix="iso-639-3-", suffix=".tab")
    with urllib.request.urlopen(SOURCE_URL) as response:  # noqa: S310 - fixed https URL
        data = response.read()
    with open(handle, "wb") as out:
        out.write(data)
    print(f"downloaded {len(data)} bytes from {SOURCE_URL}")
    return pathlib.Path(path)


def load(source: pathlib.Path) -> list[Entry]:
    """Reads the table and returns the entries, sorted by name.

    Sorting is by code point, which is what both `gofmt`ed Go string comparison
    and JavaScript's `<` on strings use, so the Go and TypeScript lists come out
    in the same order and the drift test can compare them element by element.
    """
    with open(source, encoding="utf-8", newline="") as handle:
        rows = list(csv.DictReader(handle, delimiter="\t"))

    if not rows:
        raise SystemExit(f"{source} has no rows")
    for column in ("Id", "Part1", "Ref_Name"):
        if column not in rows[0]:
            raise SystemExit(f"{source} has no {column} column; is it the ISO 639-3 table?")

    entries: list[Entry] = []
    seen_codes: set[str] = set()
    seen_names: dict[str, str] = {}

    for row in rows:
        code = row["Id"].strip()
        name = row["Ref_Name"].strip()

        if len(code) != 3 or not code.isascii() or not code.islower() or not code.isalpha():
            raise SystemExit(f"code {code!r} is not three lower-case ASCII letters")
        if not name:
            raise SystemExit(f"code {code!r} has an empty name")
        if code in seen_codes:
            raise SystemExit(f"code {code!r} appears twice")
        if name in seen_names:
            raise SystemExit(f"name {name!r} is shared by {seen_names[name]!r} and {code!r}")

        seen_codes.add(code)
        seen_names[name] = code
        entries.append(Entry(code, name))

    if len(entries) < MINIMUM_ENTRIES:
        raise SystemExit(f"only {len(entries)} entries; the table looks truncated")

    entries.sort(key=lambda entry: entry.name)
    return entries


def legacy_map(source: pathlib.Path) -> list[tuple[str, str]]:
    """Returns the ISO 639-1 -> ISO 639-3 pairs the migration needs.

    The table's `Part1` column is the ISO 639-1 code, where one exists: the 184
    languages that have a 2-letter code. Deriving the migration's mapping from
    this file rather than hand-writing it keeps the rewrite and the new list in
    agreement by construction.
    """
    with open(source, encoding="utf-8", newline="") as handle:
        rows = list(csv.DictReader(handle, delimiter="\t"))

    pairs = []
    for row in rows:
        part1 = row["Part1"].strip()
        if not part1:
            continue
        if len(part1) != 2 or not part1.isascii() or not part1.islower():
            raise SystemExit(f"Part1 value {part1!r} is not two lower-case ASCII letters")
        pairs.append((part1, row["Id"].strip()))

    targets = [code3 for _, code3 in pairs]
    if len(set(targets)) != len(targets):
        raise SystemExit("two ISO 639-1 codes map onto the same ISO 639-3 code")
    if len(set(code2 for code2, _ in pairs)) != len(pairs):
        raise SystemExit("an ISO 639-1 code appears twice")

    pairs.sort()
    return pairs


def replace_generated(path: pathlib.Path, begin: str, end: str, body: list[str]) -> None:
    """Replaces the lines between two markers, leaving the rest of the file alone."""
    original = path.read_text(encoding="utf-8").splitlines()
    try:
        start = original.index(begin)
        stop = original.index(end)
    except ValueError:
        raise SystemExit(f"{path} has no {begin.strip()!r} / {end.strip()!r} marker pair") from None

    if stop <= start:
        raise SystemExit(f"{path} has its markers in the wrong order")

    updated = original[: start + 1] + body + original[stop:]
    path.write_text("\n".join(updated) + "\n", encoding="utf-8")
    print(f"wrote {len(body)} entries into {path.relative_to(REPO_ROOT)}")


def go_quote(name: str) -> str:
    """Renders a name as a Go string literal, escaping what must be escaped."""
    return '"' + name.replace("\\", "\\\\").replace('"', '\\"') + '"'


def ts_quote(name: str) -> str:
    """Renders a name as a TypeScript literal in the style Prettier would keep.

    Prettier is configured for single quotes but switches to double quotes when a
    name contains one, because that is the form with fewer escapes. Emitting the
    same choice here means regenerating does not produce a reformat diff.
    """
    if "'" in name:
        return '"' + name.replace("\\", "\\\\").replace('"', '\\"') + '"'
    return "'" + name.replace("\\", "\\\\").replace("'", "\\'") + "'"


def go_body(entries: list[Entry]) -> list[str]:
    return [f'\t{{{go_quote(entry.code)}, {go_quote(entry.name)}}},' for entry in entries]


def ts_body(entries: list[Entry]) -> list[str]:
    return [f"  {{ code: {ts_quote(entry.code)}, name: {ts_quote(entry.name)} }}," for entry in entries]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", help="path to a downloaded iso-639-3.tab")
    arguments = parser.parse_args()

    source = pathlib.Path(arguments.source) if arguments.source else download()
    entries = load(source)
    pairs = legacy_map(source)

    replace_generated(GO_FILE, GO_BEGIN, GO_END, go_body(entries))
    replace_generated(TS_FILE, TS_BEGIN, TS_END, ts_body(entries))

    print(f"{len(entries)} languages, sorted by name; {len(pairs)} ISO 639-1 pairs for the migration")
    return 0


if __name__ == "__main__":
    sys.exit(main())
