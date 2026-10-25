/**
 * The pure formatting and filtering helpers the two Discovery screens share.
 *
 * The map's place list and the place screen's header both count a place's stories,
 * and the place header also breaks them down by pillar. Keeping that wording in one
 * place means the two screens can never disagree about how
 * "24 stories · 14 Heritage · 10 Wonder" is spelled, and the map sheet's place
 * search filters with the same rule the list it describes uses.
 *
 * Nothing here touches React or React Native, so it is exercised directly by
 * `__tests__/discoveryFormat.test.ts` — the project's renderer-less test pattern.
 * The one thing that reads a platform value is `mapTopInset`, and it takes that
 * value as an argument rather than importing it, so the rule stays pure
 * (KNOT-020g-fix9).
 */

/**
 * How far a map screen's floating bands must sit below the top of the screen.
 *
 * The discovery screens make the status bar translucent so their map can run
 * behind it, which means Android stops insetting the app: the screen's own top is
 * the physical top, and anything that must not be hidden by the bar has to pad
 * itself by the bar's height. iOS is the other way round — the root `SafeAreaView`
 * has already inset the screen past the notch, and `StatusBar.currentHeight` is
 * `undefined` there — so nothing is added (KNOT-020g-fix9).
 *
 * A missing height is read as "no inset" rather than as a failure, so a platform
 * that does not report one cannot push a band off the screen.
 */
export function mapTopInset(platform: string, statusBarHeight: number | undefined): number {
  if (platform !== 'android') {
    return 0;
  }

  return typeof statusBarHeight === 'number' && statusBarHeight > 0 ? statusBarHeight : 0;
}

/**
 * The pillars the discovery API counts, in the order the summary *line* reads them
 * (Heritage before Wonder, matching the design reference).
 *
 * This is the line's own word order, not the filter's — the two discovery screens
 * offer their pillar chips Wonder-first (see `PILLAR_TABS`). The keys are the wire
 * keys from `PlaceCluster.pillar_counts`; an unknown key is ignored rather than
 * printed, so a server that grows a third pillar cannot render a stray segment.
 */
const SUMMARY_PILLARS: readonly { readonly key: string; readonly label: string }[] = [
  { key: 'heritage', label: 'Heritage' },
  { key: 'wonder', label: 'Wonder' },
];

/**
 * The pillar filter a place page is showing: one of the two lenses, or "all".
 *
 * The values are the `?pillar=` wire values, plus "all" for the chip that sends
 * no filter, so a chip's value is exactly what the API expects once "all" is
 * dropped (KNOT-020e-fix6).
 */
export type PillarFilter = 'all' | 'heritage' | 'wonder';

/**
 * The pillar filter's options, in the order **both** discovery screens offer them
 * (All, Wonder, Heritage).
 *
 * The order is the Discovery Map's, kept here so the two screens can never drift:
 * the map's chip row and the place screen's chip row are the same control in the
 * same order (KNOT-020g-fix14). Note that the place summary *line* still reads
 * Heritage before Wonder — that is the wording the design reference prints, and it
 * is a different thing from the order of the controls.
 *
 * The choice is server-side, not a local view of the loaded page, so a chip means
 * exactly what the wire filter means.
 */
export const PILLAR_TABS: readonly { readonly value: PillarFilter; readonly label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'wonder', label: 'Wonder' },
  { value: 'heritage', label: 'Heritage' },
];

/**
 * The empty state's headline for the active pillar tab.
 *
 * A place with nothing in the chosen lens reads differently from a place with
 * nothing at all, so an empty Wonder tab does not look like a broken page
 * (KNOT-020e-fix6).
 */
export function pillarEmptyHeadline(pillar: PillarFilter): string {
  switch (pillar) {
    case 'wonder':
      return 'No Wonder stories here yet.';
    case 'heritage':
      return 'No Heritage stories here yet.';
    default:
      return 'No stories here yet.';
  }
}

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
 * The places that match a search, case-insensitively, by name.
 *
 * The search is **local**: it narrows the clusters this screen has already loaded
 * and never re-queries the server, so it can only show fewer places, never more. A
 * blank query matches everything, so clearing the field restores the whole list.
 *
 * The match is on the place's name only. The country is shown beside it, but
 * matching that too would make "Mexico" return every Mexican place, which is a
 * different question from "where is Oaxaca" (KNOT-020g-fix12).
 */
export function filterPlacesByQuery<T extends { readonly place: string }>(
  clusters: readonly T[],
  query: string,
): readonly T[] {
  const needle = query.trim().toLowerCase();
  if (needle === '') {
    return clusters;
  }

  return clusters.filter((cluster) => cluster.place.toLowerCase().includes(needle));
}

/**
 * The three heights the Discovery Map's places sheet rests at, as a share of the
 * window height: **collapsed** (half the screen), **half**, and **expanded** (nearly
 * all of it). Ascending, so the array's two ends are the sheet's travel limits
 * (KNOT-020k-fix20).
 */
export const SHEET_SNAP_RATIOS: readonly number[] = [0.5, 0.75, 0.95];

/**
 * The pixel height of a sheet ratio, rounded so the sheet lands on a whole point.
 *
 * A ratio or a window height that cannot describe a height — zero, negative, or not
 * a number — reads as 0 rather than as an arbitrary band: a sheet with no measurable
 * height is a layout that has not been measured yet, and the next frame will call
 * again.
 */
export function sheetSnapHeight(ratio: number, windowHeight: number): number {
  if (!Number.isFinite(ratio) || !Number.isFinite(windowHeight) || windowHeight <= 0) {
    return 0;
  }

  return Math.round(ratio * windowHeight);
}

/**
 * Holds a ratio inside the sheet's travel, between its collapsed and expanded
 * points, so a long drag can never pull the sheet off either edge of the screen
 * (KNOT-020k-fix20).
 */
export function clampSheetRatio(ratio: number): number {
  const lowest = SHEET_SNAP_RATIOS[0];
  const highest = SHEET_SNAP_RATIOS[SHEET_SNAP_RATIOS.length - 1];

  if (!Number.isFinite(ratio)) {
    return lowest;
  }

  return Math.min(highest, Math.max(lowest, ratio));
}

/**
 * The snap point a release lands on: the nearest one.
 *
 * A tie keeps the **lower** (shorter) point, so a drag that stops exactly halfway
 * between two points settles back rather than jumping the sheet taller than the
 * finger asked for (KNOT-020k-fix20).
 */
export function nearestSheetRatio(ratio: number): number {
  const target = clampSheetRatio(ratio);
  let nearest = SHEET_SNAP_RATIOS[0];
  let nearestDistance = Math.abs(target - nearest);

  for (const candidate of SHEET_SNAP_RATIOS) {
    const distance = Math.abs(target - candidate);
    if (distance < nearestDistance) {
      nearest = candidate;
      nearestDistance = distance;
    }
  }

  return nearest;
}

/**
 * The **live** ratio while a finger is down: the resting ratio moved by `dy` points
 * (negative is upward), clamped to the travel but not snapped — the sheet follows
 * the finger, and only the release picks a point (KNOT-020k-fix20).
 */
export function sheetRatioDuringDrag(startRatio: number, dy: number, windowHeight: number): number {
  if (!Number.isFinite(windowHeight) || windowHeight <= 0) {
    return clampSheetRatio(startRatio);
  }

  return clampSheetRatio(startRatio - dy / windowHeight);
}

/**
 * Where a released drag settles: the snap point nearest where the finger let go
 * (KNOT-020k-fix20).
 */
export function sheetRatioAfterDrag(startRatio: number, dy: number, windowHeight: number): number {
  return nearestSheetRatio(sheetRatioDuringDrag(startRatio, dy, windowHeight));
}

/**
 * The snap point one step above (`1`) or below (`-1`) a ratio, for the handle's
 * accessibility actions. A ratio that is not exactly on a point starts from the
 * point nearest it, and either end of the travel stays put (KNOT-020k-fix20).
 */
export function adjacentSheetRatio(ratio: number, direction: -1 | 1): number {
  const exact = SHEET_SNAP_RATIOS.indexOf(ratio);
  const current = exact === -1 ? SHEET_SNAP_RATIOS.indexOf(nearestSheetRatio(ratio)) : exact;
  const next = Math.min(Math.max(current + direction, 0), SHEET_SNAP_RATIOS.length - 1);

  return SHEET_SNAP_RATIOS[next];
}
