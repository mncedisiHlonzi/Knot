import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StatusBar,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from 'react-native';
import Mapbox, { Camera, MapView, PointAnnotation } from '@rnmapbox/maps';
import { CaretRight, MagnifyingGlass, MapPin, MapPinLine, X } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { PlaceCluster, discoveryApi } from '../../api/discovery';
import { Pillar } from '../../api/stories';
import Chip from '../../components/Chip';
import EmptyState from '../../components/EmptyState';
import Input from '../../components/Input';
import { KNOT_MAPBOX_TOKEN } from '../../config/dev';
import { PlaceCoordinate, coordinatesForPlace } from '../../data/placeCoordinates';
import {
  elevation,
  layout,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { ThemeColors } from '../../theme';
import { filterPlacesByQuery, placeTotalsLabel, storyCountLabel } from './discoveryFormat';

type DiscoveryMapScreenProps = {
  /**
   * Called when a place is tapped, on the map or in the list below it. The whole
   * cluster is handed over, so the place screen can name itself and print its
   * story and pillar totals without a second request.
   */
  readonly onOpenPlace: (cluster: PlaceCluster) => void;
  /**
   * The signed-in user's access token, or undefined when signed out. It is sent
   * on the cluster request so the server hides blocked authors' stories; an
   * anonymous request sees every place.
   */
  readonly token?: string;
};

/** The pillar filter, plus "all". */
type PillarFilter = 'all' | Pillar;

/** The filter options, in the order they are offered. */
const FILTERS: readonly { readonly value: PillarFilter; readonly label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'wonder', label: 'Wonder' },
  { value: 'heritage', label: 'Heritage' },
];

/**
 * A world-scale starting camera, so every plotted place is on screen.
 *
 * Mapbox positions are `[longitude, latitude]` (GeoJSON order) — the opposite of
 * the latitude/longitude object the previous react-native-maps screen used.
 */
const INITIAL_CENTER: [number, number] = [10, 10];
const INITIAL_ZOOM = 1.2;

/** The location pin's glyph size in a place row. */
const ICON_SIZE = 20;

/** The magnifier inside the place search field. */
const SEARCH_ICON_SIZE = 18;

/**
 * The map marker: one filled indigo pin per place.
 *
 * It carries no story count. A count inside a marker made every place look like a
 * badge to read rather than a place to tap, and it forced the markers to change
 * size with the busiest cluster; a single pin shape reads as "a place is here" at
 * a glance, whatever it holds (KNOT-020g-fix12).
 */
const MARKER_ICON_SIZE = 30;

/**
 * The marker's touch target.
 *
 * The pin is invisible at touch level, so it is centred in a 44-point box — the
 * design system's minimum target — and the box is transparent, so the pin still
 * looks like a pin (KNOT-020g-fix12).
 */
const MARKER_TOUCH_SIZE = 44;

/**
 * The share of the screen height the bottom sheet occupies.
 *
 * The sheet carries the whole screen's chrome now — the headline, the pillar
 * filter and the place list — so it takes half the screen rather than the 44% it
 * held when a header and a tab row floated above it (KNOT-020g-fix10). The map
 * keeps the rest and still extends behind the sheet, so the sheet's own height is
 * the only thing this controls.
 */
const SHEET_HEIGHT_RATIO = 0.5;

// Mapbox needs its access token before any map mounts. Setting it once, at module
// load, keeps it out of the component and off every render. The token is public
// (see `config/dev.ts`).
void Mapbox.setAccessToken(KNOT_MAPBOX_TOKEN);

/**
 * Resolves a cluster to a point the map can plot.
 *
 * A story that carries a structured coordinate plots at exactly that point, so
 * every place a user picked from the geocoding search appears on the map
 * (KNOT-ADR-034). A legacy cluster without a coordinate falls back to the local
 * lookup table, so stories created before migration 0009 are not lost.
 */
function coordinateForCluster(cluster: PlaceCluster): PlaceCoordinate | undefined {
  if (cluster.latitude !== null && cluster.longitude !== null) {
    return { latitude: cluster.latitude, longitude: cluster.longitude };
  }
  return coordinatesForPlace(cluster.place);
}

/**
 * The quiet line beneath a place in the list: the country the geocoder reported,
 * and a note when the place has no coordinate to plot. It is empty when there is
 * nothing to say, so the row draws one line rather than a blank second one.
 */
function placeSubtitle(cluster: PlaceCluster): string {
  const country = typeof cluster.place_country === 'string' ? cluster.place_country.trim() : '';
  const parts = country === '' ? [] : [country];

  if (coordinateForCluster(cluster) === undefined) {
    parts.push('Not on map');
  }

  return parts.join(' · ');
}

/**
 * A stable key for a cluster across the map and the list.
 *
 * The place's country is part of its identity (KNOT-020e-fix), so two clusters
 * that share a name but differ in country — or in having none — must not collide
 * as React keys. Both fields are joined by a character no place name can contain.
 */
function clusterKey(cluster: PlaceCluster): string {
  return `${cluster.place}\u0000${cluster.place_country ?? ''}`;
}

/**
 * Discovery, as a map.
 *
 * The map is rendered natively with Mapbox (`@rnmapbox/maps`, KNOT-ADR-026). The
 * server groups stories by place and stores the coordinate a story was given
 * (from the geocoding picker, KNOT-ADR-034); this screen plots that coordinate
 * directly. A cluster with no coordinate — a story created before structured
 * place data existed — falls back to the local lookup table
 * (`src/data/placeCoordinates.ts`), and a place in neither is listed below the
 * map rather than hidden.
 *
 * Tapping a marker, or an entry in the list, opens that place's stories. There is
 * no user location and no permission prompt: the map is a way in to places, not a
 * way to find the user.
 *
 * The layout is **map-first** (KNOT-020g-fix10): the map IS the screen, edge to
 * edge and running behind the translucent status bar, and there is no header, no
 * title and no tab row above it. Everything the screen offers lives in the bottom
 * sheet — a drag handle, the headline, the pillar filter as a chip row, a search
 * over the places already loaded, the aggregate totals, and the scrollable place
 * list — which is the shape Bolt's and Uber's home screens use. The filter is the
 * same server-side query it always was; only the control that sets it moved.
 *
 * The search is **local**: it narrows the clusters this screen has already loaded
 * and never re-queries the server, so it can only ever show fewer places than the
 * filter returned. A server-side place search would be a follow-up. The
 * reference's *map* search control (a floating button over the map) and its
 * location button are still not built — the location button would need a
 * permission and a behaviour the app does not have — and the sheet's drag handle
 * is decorative for the MVP, with no drag-to-expand (KNOT-020g-fix12).
 */
export default function DiscoveryMapScreen({
  onOpenPlace,
  token,
}: DiscoveryMapScreenProps): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [clusters, setClusters] = useState<readonly PlaceCluster[]>([]);
  const [filter, setFilter] = useState<PillarFilter>('all');
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const { height } = useWindowDimensions();
  const sheetHeight = Math.round(height * SHEET_HEIGHT_RATIO);

  const load = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'refresh') {
        setRefreshing(true);
      } else {
        setLoading(true);
      }
      setError(undefined);

      try {
        const result = await discoveryApi.listClusters(
          filter === 'all' ? {} : { pillar: filter },
          token,
        );
        setClusters(result.clusters);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [filter, token],
  );

  useEffect(() => {
    void load('initial');
  }, [load]);

  /** Pull-to-refresh: re-read the first page for the filter that is showing. */
  const refresh = useCallback((): void => {
    void load('refresh');
  }, [load]);

  // The places the search leaves. It is a local filter over the clusters already
  // loaded — the sheet never re-queries the server — so a search can only narrow
  // what the pillar filter returned (KNOT-020g-fix12). Both the list and the map
  // read it, so the markers can never disagree with the rows.
  const visible = useMemo(() => filterPlacesByQuery(clusters, query), [clusters, query]);

  // The clusters the map can actually plot. A cluster with no coordinate is still
  // listed below the map, so a place told before structured place data existed is
  // never hidden.
  const plotted = useMemo(
    () => visible.filter((cluster) => coordinateForCluster(cluster) !== undefined),
    [visible],
  );

  // The sheet header's totals, summed over the places the sheet is showing, so the
  // line describes exactly the list beneath it — including after a search. The
  // endpoint returns one page (up to its cluster limit), so these are the totals
  // for what is shown, not necessarily for every place in the world
  // (KNOT-020e-fix).
  const totals = useMemo(() => {
    let stories = 0;
    const pillars: Record<string, number> = {};

    for (const cluster of visible) {
      stories += cluster.story_count;
      for (const [pillar, count] of Object.entries(cluster.pillar_counts)) {
        pillars[pillar] = (pillars[pillar] ?? 0) + count;
      }
    }

    return { places: visible.length, stories, pillars };
  }, [visible]);

  // A search that matched nothing is told apart from a place with no stories, so
  // the sheet can say which of the two happened.
  const searching = query.trim() !== '';

  return (
    <View style={styles.container}>
      {/* A **transparent** status bar on this screen only, so the map shows
          through it — the map-first look (KNOT-020g-fix10). The icons stay dark,
          which reads on Mapbox's pale Light basemap. Nothing else on the screen
          has to clear the bar: there is no header or tab row above the map, and
          the sheet is pinned to the bottom. The app-wide bar takes over again on
          unmount. */}
      <StatusBar
        backgroundColor="transparent"
        barStyle={theme === 'dark' ? 'light-content' : 'dark-content'}
        translucent
      />

      {/*
       * The map is drawn natively with Mapbox (`@rnmapbox/maps`), superseding
       * react-native-maps + OpenStreetMap (KNOT-ADR-026). OSM's tile server
       * returns an `x-blocked` header to anonymous clients and react-native-maps
       * cannot send identifying headers, so its tiles never rendered; Mapbox draws
       * tiles from its own native SDK using an access token.
       *
       * It is the **whole screen**, edge to edge and from the physical top behind
       * the status bar to the bottom behind the sheet, which is the whole point of
       * the layout (KNOT-020g-fix9, KNOT-020g-fix10).
       *
       * The token is the *public* one from `src/config/dev.ts`, and the Light style
       * keeps the map reading as the reference's light canvas (KNOT-020e-fix). The
       * scale bar is off — it is not part of the design. The logo and the
       * attribution button are both **on**, as Mapbox's terms require, and both sit
       * on the one line of map the sheet does not cover: a lower corner of this map
       * is behind the sheet, and a mark the user cannot see satisfies nothing
       * (KNOT-020g-fix10).
       */}
      <MapView
        style={styles.map}
        styleURL={theme === 'dark' ? Mapbox.StyleURL.Dark : Mapbox.StyleURL.Light}
        scaleBarEnabled={false}
        attributionEnabled
        attributionPosition={{ bottom: sheetHeight + spacing.sm, right: spacing.sm }}
        logoEnabled
        logoPosition={{ bottom: sheetHeight + spacing.sm, left: spacing.sm }}
      >
        <Camera defaultSettings={{ centerCoordinate: INITIAL_CENTER, zoomLevel: INITIAL_ZOOM }} />
        {plotted.map((cluster) => {
          const coordinate = coordinateForCluster(cluster);
          if (coordinate === undefined) {
            return null;
          }

          return (
            <PointAnnotation
              key={clusterKey(cluster)}
              id={clusterKey(cluster)}
              coordinate={[coordinate.longitude, coordinate.latitude]}
              title={cluster.place}
              onSelected={() => onOpenPlace(cluster)}
            >
              {/* PointAnnotation renders its child as the marker: one filled indigo
                  pin per place, with no count inside it and no disc behind it. The
                  pin is centred in a transparent 44-point box so it keeps the
                  design system's minimum touch target without looking like a
                  button (KNOT-020g-fix12). */}
              <View style={styles.marker}>
                <MapPin color={colors.brand.primary} size={MARKER_ICON_SIZE} weight="fill" />
              </View>
            </PointAnnotation>
          );
        })}
      </MapView>

      {/* Everything the screen offers, in one sheet over the map's lower half: the
          drag handle, the headline, the pillar filter as a chip row, the aggregate
          totals, then the scrollable place list. The sheet is opaque and rounded,
          so the map reads as the backdrop rather than as a layer the content is
          fighting (KNOT-020g-fix10). Its `zIndex` keeps it above the map. */}
      <View style={[styles.sheet, { height: sheetHeight }]}>
        <View style={styles.handle} />

        <View style={styles.sheetHeader}>
          <Text style={[typeScale.h1, styles.headline]}>Discover places.</Text>
          <Text style={[typeScale.body, styles.subtitle]}>
            Explore stories by place around the world.
          </Text>

          {/* The pillar filter, moved here from the tab row that used to float
              above the map. It is the same server-side query it always was: a chip
              is a `?pillar=` value, and "All" sends none (KNOT-020e-fix6). */}
          <View style={styles.chipRow}>
            {FILTERS.map((option) => (
              <Chip
                key={option.value}
                label={option.label}
                selected={option.value === filter}
                onPress={() => setFilter(option.value)}
              />
            ))}
          </View>

          {/* The place search. It filters the places this sheet has already
              loaded — the clusters the pillar filter returned — and never
              re-queries the server, so it can only narrow what is on screen
              (KNOT-020g-fix12). The clear control is a sibling rather than a prop
              on `Input`, which has one leading slot and no trailing one. */}
          <View style={styles.searchRow}>
            <Input
              value={query}
              onChangeText={setQuery}
              placeholder="Search places"
              leadingIcon={<MagnifyingGlass color={colors.text.tertiary} size={SEARCH_ICON_SIZE} />}
              autoCapitalize="none"
              autoCorrect={false}
              returnKeyType="search"
              accessibilityLabel="Search places"
            />
            {query === '' ? null : (
              <Pressable
                style={styles.searchClear}
                onPress={() => setQuery('')}
                hitSlop={spacing.sm}
                accessibilityRole="button"
                accessibilityLabel="Clear the place search"
              >
                <X color={colors.text.tertiary} size={16} weight="bold" />
              </Pressable>
            )}
          </View>
        </View>

        {/* What the list below holds, in the same wording the sheet has always
            used — recomputed over the searched places, so it describes the rows
            under it rather than a count the reader cannot see. */}
        <Text style={[typeScale.caption, styles.totals]}>
          {placeTotalsLabel(totals.places, totals.stories, totals.pillars)}
        </Text>

        <FlatList
          style={styles.list}
          data={visible}
          keyExtractor={(cluster) => clusterKey(cluster)}
          contentContainerStyle={styles.listContent}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              onRefresh={refresh}
              tintColor={colors.text.secondary}
            />
          }
          ListHeaderComponent={
            loading ? (
              <ActivityIndicator style={styles.spinner} />
            ) : error !== undefined ? (
              <Text style={styles.error}>{error}</Text>
            ) : null
          }
          renderItem={({ item }) => {
            const subtitle = placeSubtitle(item);

            return (
              <Pressable
                style={styles.placeRow}
                onPress={() => onOpenPlace(item)}
                accessibilityRole="button"
                accessibilityLabel={`${item.place}, ${storyCountLabel(item.story_count)}`}
              >
                {/* The location pin, drawn as a **line** glyph (not a filled one)
                    and with no disc behind it, so it reads as a quiet marker for
                    the row rather than as a button — the sheet's own chips are the
                    only filled shapes in it (KNOT-020g-fix12). A place is a place
                    whether or not it plots, so the pin is on every row rather than
                    only on the ones the map drew. */}
                <MapPinLine color={colors.text.tertiary} size={ICON_SIZE} />

                <View style={styles.placeText}>
                  <Text style={[typeScale.h3, styles.placeName]} numberOfLines={1}>
                    {item.place}
                  </Text>
                  {subtitle === '' ? null : (
                    <Text style={[typeScale.caption, styles.placeMeta]} numberOfLines={1}>
                      {subtitle}
                    </Text>
                  )}
                </View>

                <View style={styles.placeCount}>
                  <Text style={[typeScale.caption, tabularNums, styles.placeCountText]}>
                    {storyCountLabel(item.story_count)}
                  </Text>
                  <CaretRight color={colors.text.tertiary} size={18} />
                </View>
              </Pressable>
            );
          }}
          ListEmptyComponent={
            !loading && error === undefined ? (
              searching ? (
                <Text style={styles.noMatches}>No places match</Text>
              ) : (
                <EmptyState
                  illustrationName="no-stories"
                  headline="No places yet."
                  body="When people tell stories about a place, it appears on the map and in this list."
                />
              )
            ) : null
          }
        />
      </View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The pillar filter as a row of chips, under the headline. It wraps rather than
    // scrolls: three short labels fit one line on any phone, and a row that could
    // not wrap would clip a fourth pillar if the server ever grows one
    // (KNOT-020g-fix10).
    chipRow: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
      marginTop: spacing.lg,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    error: {
      color: colors.state.danger,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
      textAlign: 'center',
    },
    // The decorative drag handle: a 32x4 pill in the hairline colour. No gesture is
    // attached — drag-to-expand is out of scope for the MVP (KNOT-020e-fix).
    handle: {
      alignSelf: 'center',
      backgroundColor: colors.border.hairline,
      borderRadius: radius.pill,
      height: 4,
      marginBottom: spacing.lg,
      marginTop: spacing.md,
      width: 32,
    },
    // The sheet's headline: the screen's only large type now that the title bar is
    // gone, so it takes the display face at h1 (KNOT-020g-fix10).
    headline: {
      color: colors.text.primary,
    },
    list: {
      flex: 1,
    },
    listContent: {
      paddingBottom: spacing.xxxl,
    },
    // The map is the screen, not a band in it: it spans from the top edge — behind
    // the status bar — to the bottom, and everything else floats over it
    // (KNOT-020g-fix9).
    map: {
      ...StyleSheet.absoluteFillObject,
    },
    // The map marker's touch target: a transparent 44-point box with one filled pin
    // centred in it. The box carries no fill or ring, so the marker reads as a pin on
    // the map rather than as a badge (KNOT-020g-fix12).
    marker: {
      alignItems: 'center',
      height: MARKER_TOUCH_SIZE,
      justifyContent: 'center',
      width: MARKER_TOUCH_SIZE,
    },
    // The line drawn under a search that matched nothing, in the list's own inset.
    noMatches: {
      color: colors.text.tertiary,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.lg,
      textAlign: 'center',
    },
    placeCount: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.xs,
    },
    // The count is the quieter half of the row: the place's name is what a reader is
    // scanning for, so the number recedes to the tertiary ink (KNOT-020g-fix11).
    placeCountText: {
      color: colors.text.tertiary,
    },
    // The leading location pin sits straight on the row's canvas: there is no disc
    // behind it, so the row is a plain icon-text-count line (KNOT-020g-fix12).
    placeMeta: {
      color: colors.text.tertiary,
      marginTop: spacing.xs,
    },
    placeName: {
      color: colors.text.primary,
    },
    // One place. **No separator**: the rows are told apart by their own height and
    // padding, which is the sheet's pattern — a rule between them is the one thing
    // the reference does not draw (KNOT-020g-fix11).
    placeRow: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    // The name and country take every point between the pin and the count, so they
    // are **left-aligned** beside the pin and the count sits at the far right. Before
    // this the column hugged its text, which pushed it into the middle of the row and
    // made a row look centred (KNOT-020g-fix12).
    placeText: {
      flex: 1,
      flexShrink: 1,
    },
    // The place search, above the list it narrows. The clear control is absolutely
    // positioned over the field's right edge, so it cannot shift the field's width
    // when it appears (KNOT-020g-fix12).
    searchClear: {
      alignItems: 'center',
      bottom: 0,
      justifyContent: 'center',
      position: 'absolute',
      right: spacing.sm,
      top: 0,
      width: 24,
    },
    searchRow: {
      marginTop: spacing.lg,
    },
    // The sheet: rounded top corners, the canvas surface, and the design's upward
    // shadow (elevation Level 3, whose offset-Y is negative — exactly this use
    // case). Pinned to the bottom so it overlaps the map's lower edge. No
    // `overflow: hidden`, so the shadow is not clipped; the corners are above the
    // list, which never reaches them (KNOT-020e-fix). `zIndex` keeps it above the
    // map, which now has nothing else over it in this screen's stack
    // (KNOT-020g-fix9, KNOT-020g-fix10).
    sheet: {
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      bottom: 0,
      left: 0,
      position: 'absolute',
      right: 0,
      zIndex: 1,
      ...elevation[3],
    },
    // The sheet's fixed head: everything above the list, which never scrolls away
    // with it.
    sheetHeader: {
      paddingBottom: spacing.sm,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    subtitle: {
      color: colors.text.secondary,
      marginTop: spacing.xs,
    },
    spinner: {
      marginTop: spacing.lg,
    },
    // What the list below holds. Indented to the screen's gutter like the rows it
    // describes, so the two read as one block (KNOT-020g-fix10).
    totals: {
      color: colors.text.tertiary,
      paddingBottom: spacing.sm,
      paddingHorizontal: layout.screenPaddingFeed,
    },
  });
