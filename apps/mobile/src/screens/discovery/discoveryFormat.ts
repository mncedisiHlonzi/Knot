/**
 * The pure formatting and sizing helpers the two Discovery screens share.
 *
 * The map's place list and the place screen's header both count a place's stories,
 * and the place header also breaks them down by pillar. Keeping that wording in one
 * place means the two screens can never disagree about how
 * "24 stories · 14 Heritage · 10 Wonder" is spelled, and the map's marker geometry
 * stays out of the component that draws it.
 *
 * Nothing here touches React or React Native, so it is exercised directly by
 * `__tests__/discoveryFormat.test.ts` — the project's renderer-less test pattern.
 */

/**
 * The pillars the discovery API counts, in the order the summary line reads them
 * (Heritage before Wonder, matching the design reference).
 *
 * The keys are the wire keys from `PlaceCluster.pillar_counts`; an unknown key is
 * ignored rather than printed, so a server that grows a third pillar cannot render
 * a stray segment.
 */
const SUMMARY_PILLARS: readonly { readonly key: string; readonly label: string }[] = [
  { key: 'heritage', label: 'Heritage' },
  { key: 'wonder', label: 'Wonder' },
];

/**
 * The smallest and largest map-marker diameters, in points. A cluster's marker
 * scales between them by how its story count compares with the busiest cluster on
 * screen, so a place with many stories reads larger at a glance.
 */
export const MARKER_MIN_SIZE = 28;
export const MARKER_MAX_SIZE = 44;

/**
 * Renders a story count with the right noun: "1 story" / "24 stories".
 */
export function storyCountLabel(count: number): string {
  return count === 1 ? '1 story' : `${count} stories`;
}

/**
 * Renders a place count with the right noun: "1 place" / "5 places". Used by the
 * Discovery Map sheet header, which totals every place it has loaded.
 */
export function placeCountLabel(count: number): string {
  return count === 1 ? '1 place' : `${count} places`;
}

/**
 * The pillar segments of a summary line, in reading order, each left out when its
 * count is zero or absent so a wonder-only place never prints "0 Heritage".
 */
function pillarSegments(pillarCounts: Readonly<Record<string, number>>): string[] {
  const segments: string[] = [];

  for (const { key, label } of SUMMARY_PILLARS) {
    const count = pillarCounts[key];
    if (typeof count === 'number' && count > 0) {
      segments.push(`${count} ${label}`);
    }
  }

  return segments;
}

/**
 * A place's summary line: "{N} stories · {H} Heritage · {W} Wonder".
 *
 * A pillar with no stories is left out rather than printed as a zero, so a place
 * whose stories are all wonder reads "{N} stories · {W} Wonder".
 */
export function placeSummaryLabel(
  storyCount: number,
  pillarCounts: Readonly<Record<string, number>>,
): string {
  return [storyCountLabel(storyCount), ...pillarSegments(pillarCounts)].join(' · ');
}

/**
 * The Discovery Map sheet header's totals: "{P} places · {N} stories · {H}
 * Heritage · {W} Wonder".
 *
 * The totals are summed over the clusters the sheet has loaded, so they describe
 * exactly the list beneath them. A pillar with no stories is left out.
 */
export function placeTotalsLabel(
  placeCount: number,
  storyCount: number,
  pillarCounts: Readonly<Record<string, number>>,
): string {
  const segments = [placeCountLabel(placeCount), storyCountLabel(storyCount)];
  return [...segments, ...pillarSegments(pillarCounts)].join(' · ');
}

/**
 * A point the map can plot. It mirrors the shape `../data/placeCoordinates`
 * exposes, so a value from either source is interchangeable.
 */
export type PlaceMapPoint = {
  readonly latitude: number;
  readonly longitude: number;
};

/**
 * Reports whether a value is a usable coordinate within `bound` degrees.
 *
 * A type predicate, so a guarded pair narrows to plain numbers without a cast.
 */
function isCoordinate(value: number | null | undefined, bound: number): value is number {
  return typeof value === 'number' && Number.isFinite(value) && Math.abs(value) <= bound;
}

/**
 * The point the Place Stories map strip plots, or `undefined` when the strip must
 * not be drawn at all.
 *
 * A cluster's coordinate is nullable — a place told only by name has none — and a
 * non-finite or out-of-range value would be rejected by the native map, so the
 * point is returned only when both numbers are present, finite, and inside the
 * coordinate range. Anything else means "no strip" rather than a marker dropped
 * at [0°, 0°] in the Gulf of Guinea (KNOT-020e-fix3).
 */
export function placeMapPoint(
  latitude: number | null | undefined,
  longitude: number | null | undefined,
): PlaceMapPoint | undefined {
  if (!isCoordinate(latitude, 90) || !isCoordinate(longitude, 180)) {
    return undefined;
  }

  return { latitude, longitude };
}

/**
 * The diameter of a cluster's map marker.
 *
 * The busiest cluster takes the largest marker and every other cluster scales
 * linearly towards the smallest, so the sizes stay comparable within one screen
 * (and re-scale when a pillar filter changes which clusters are plotted). A
 * missing or nonsensical count falls back to the smallest marker rather than
 * dividing by zero.
 */
export function markerSize(count: number, maxCount: number): number {
  const safeMax = Number.isFinite(maxCount) && maxCount > 0 ? maxCount : 0;
  if (safeMax === 0) {
    return MARKER_MIN_SIZE;
  }

  const safeCount = Number.isFinite(count) && count > 0 ? count : 0;
  const ratio = Math.min(safeCount / safeMax, 1);

  return Math.round(MARKER_MIN_SIZE + (MARKER_MAX_SIZE - MARKER_MIN_SIZE) * ratio);
}

/**
 * The number drawn inside a map marker, capped at "99+" so a busy place cannot
 * overflow its circle.
 */
export function markerCountLabel(count: number): string {
  return count > 99 ? '99+' : String(count);
}
