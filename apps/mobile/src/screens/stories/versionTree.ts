/**
 * The Language Tree's read model: turning the server's flat adjacency list into
 * the rows the screen draws, in the order a tree reads.
 *
 * The server returns every version of a story with its `parent_version_id`; the
 * client assembles the nesting (KNOT-ADR-011). This module owns that assembly so
 * the tree's depth, order, and counts are pure and testable — the screen itself
 * only lays out the result (KNOT-020d).
 */
import type { StoryVersion } from '../../api/versions';

/** One drawn row of the tree. */
export type TreeRow = {
  readonly version: StoryVersion;
  /** 0 for the root, 1 for its children, and so on. */
  readonly depth: number;
  /** How many versions were adapted from this one. */
  readonly adaptCount: number;
};

/**
 * Flattens a story's versions into the order the tree draws them: a depth-first
 * walk from each root, so a parent is immediately above its children and each
 * child is indented under the parent it came from.
 *
 * A version whose parent is absent from the list is treated as a root too, so a
 * partial tree still renders. The walk is bounded by a seen-set so a malformed
 * parent cycle (which the schema forbids) can never hang the screen; anything left
 * unreached is appended rather than hidden.
 */
export function treeRows(versions: readonly StoryVersion[]): readonly TreeRow[] {
  const childrenOf = new Map<string, StoryVersion[]>();
  for (const version of versions) {
    const parent = version.parent_version_id;
    if (parent === null) {
      continue;
    }
    const siblings = childrenOf.get(parent);
    if (siblings === undefined) {
      childrenOf.set(parent, [version]);
    } else {
      siblings.push(version);
    }
  }

  const ids = new Set(versions.map((version) => version.id));
  const roots = versions.filter(
    (version) => version.parent_version_id === null || !ids.has(version.parent_version_id),
  );

  const rows: TreeRow[] = [];
  const seen = new Set<string>();

  function walk(version: StoryVersion, depth: number): void {
    if (seen.has(version.id)) {
      return;
    }
    seen.add(version.id);

    const children = childrenOf.get(version.id) ?? [];
    rows.push({ version, depth, adaptCount: children.length });
    for (const child of children) {
      walk(child, depth + 1);
    }
  }

  for (const root of roots) {
    walk(root, 0);
  }
  for (const version of versions) {
    if (!seen.has(version.id)) {
      walk(version, 1);
    }
  }

  return rows;
}

/** The counts and title the tree's header shows. */
export type TreeSummary = {
  /** The story's title, which is its root version's title. */
  readonly title: string;
  readonly versionCount: number;
  readonly languageCount: number;
};

/** Summarises a tree for the screen's subtitle line. */
export function treeSummary(rows: readonly TreeRow[]): TreeSummary {
  const root = rows.find((row) => row.depth === 0) ?? rows[0];

  return {
    title: root === undefined ? '' : root.version.title,
    versionCount: rows.length,
    languageCount: new Set(rows.map((row) => row.version.language)).size,
  };
}

/** How many adaptations a version drew: "4 adaptations", "1 adaptation". */
export function adaptCountLabel(count: number): string {
  return `${count} ${count === 1 ? 'adaptation' : 'adaptations'}`;
}
