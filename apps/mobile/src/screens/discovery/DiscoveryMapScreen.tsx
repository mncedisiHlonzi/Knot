import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Platform,
  Pressable,
  RefreshControl,
  StatusBar,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from 'react-native';
import Mapbox, { Camera, MapView, PointAnnotation } from '@rnmapbox/maps';
import { CaretRight } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { PlaceCluster, discoveryApi } from '../../api/discovery';
import { Pillar } from '../../api/stories';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import { KNOT_MAPBOX_TOKEN } from '../../config/dev';
import { PlaceCoordinate, coordinatesForPlace } from '../../data/placeCoordinates';
import { colors, elevation, layout, radius, spacing, tabularNums, typeScale } from '../../theme';
import {
  mapTopInset,
  markerCountLabel,
  markerSize,
  placeTotalsLabel,
  storyCountLabel,
} from './discoveryFormat';

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

/**
 * The share of the screen height the bottom sheet occupies. The reference gives
 * the map the screen and the sheet the bottom of it, overlapping the map's lower
 * edge (KNOT-020e-fix). The map behind the sheet is not sized explicitly: it
 * simply fills the space between the tabs and the bottom of the screen, so it
 * always extends behind the sheet whatever the device's height.
 */
const SHEET_HEIGHT_RATIO = 0.44;

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
 * The layout is the design reference's (KNOT-020e, KNOT-020e-fix): a static header
 * and the pillar tabs, the map filling the screen behind a rounded bottom sheet,
 * and the sheet carrying the aggregate totals and the scrollable place list over
 * the map's lower edge. The reference's map search control and its location
 * button are deliberately not built — the location button would need a permission
 * and a behaviour the app does not have — and the sheet's drag handle is
 * decorative for the MVP, with no drag-to-expand.
 */
export default function DiscoveryMapScreen({
  onOpenPlace,
  token,
}: DiscoveryMapScreenProps): React.ReactElement {
  const [clusters, setClusters] = useState<readonly PlaceCluster[]>([]);
  const [filter, setFilter] = useState<PillarFilter>('all');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const { height } = useWindowDimensions();
  const sheetHeight = Math.round(height * SHEET_HEIGHT_RATIO);
  // The status bar is translucent here so the map runs behind it, which means the
  // floating bands below it have to keep clear of it themselves (KNOT-020g-fix9).
  const topInset = mapTopInset(Platform.OS, StatusBar.currentHeight);

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

  // The clusters the map can actually plot. A cluster with no coordinate is still
  // listed below the map, so a place told before structured place data existed is
  // never hidden.
  const plotted = useMemo(
    () => clusters.filter((cluster) => coordinateForCluster(cluster) !== undefined),
    [clusters],
  );

  // The busiest plotted cluster sets the scale every marker is sized against.
  const maxCount = useMemo(
    () => plotted.reduce((max, cluster) => Math.max(max, cluster.story_count), 0),
    [plotted],
  );

  // The sheet header's totals, summed over every cluster the sheet has loaded, so
  // the line describes exactly the list beneath it. The endpoint returns one page
  // (up to its cluster limit), so these are the totals for what is shown, not
  // necessarily for every place in the world (KNOT-020e-fix).
  const totals = useMemo(() => {
    let stories = 0;
    const pillars: Record<string, number> = {};

    for (const cluster of clusters) {
      stories += cluster.story_count;
      for (const [pillar, count] of Object.entries(cluster.pillar_counts)) {
        pillars[pillar] = (pillars[pillar] ?? 0) + count;
      }
    }

    return { places: clusters.length, stories, pillars };
  }, [clusters]);

  return (
    <View style={styles.container}>
      {/* The status bar goes translucent on this screen only, so the map can run
          behind it. Because the app is then laid out from the physical top, the
          bar is painted opaque in the canvas colour — the map is behind it, and
          the icons stay dark on white rather than over the map (KNOT-020g-fix9). */}
      <StatusBar backgroundColor={colors.bg.canvas} barStyle="dark-content" translucent />

      {/*
       * The map is drawn natively with Mapbox (`@rnmapbox/maps`), superseding
       * react-native-maps + OpenStreetMap (KNOT-ADR-026). OSM's tile server
       * returns an `x-blocked` header to anonymous clients and react-native-maps
       * cannot send identifying headers, so its tiles never rendered; Mapbox draws
       * tiles from its own native SDK using an access token.
       *
       * It is the **whole screen**, from the top edge behind the status bar to the
       * bottom, with the header, the tabs and the sheet floating over it
       * (KNOT-020g-fix9).
       *
       * The token is the *public* one from `src/config/dev.ts`. The Light style is
       * used so the map reads as the reference's light canvas (KNOT-020e-fix), and
       * Mapbox's logo is left enabled, as its terms require. The scale bar and the
       * attribution button are off: neither is part of the design, and the logo
       * alone is the mark the terms mandate (KNOT-020g-fix9). The logo sits just
       * above the sheet, because every lower corner of the map is behind it and a
       * hidden logo would not satisfy those terms at all.
       */}
      <MapView
        style={styles.map}
        styleURL={Mapbox.StyleURL.Light}
        scaleBarEnabled={false}
        attributionEnabled={false}
        logoEnabled
        logoPosition={{ bottom: sheetHeight + spacing.sm, left: spacing.sm }}
      >
        <Camera defaultSettings={{ centerCoordinate: INITIAL_CENTER, zoomLevel: INITIAL_ZOOM }} />
        {plotted.map((cluster) => {
          const coordinate = coordinateForCluster(cluster);
          if (coordinate === undefined) {
            return null;
          }

          const size = markerSize(cluster.story_count, maxCount);

          return (
            <PointAnnotation
              key={clusterKey(cluster)}
              id={clusterKey(cluster)}
              coordinate={[coordinate.longitude, coordinate.latitude]}
              title={cluster.place}
              onSelected={() => onOpenPlace(cluster)}
            >
              {/* PointAnnotation renders its child as the marker: the place's story
                  count in an indigo circle, ringed in the navy ink so it reads on
                  the light map. The count is now the whole place's, not one
                  coordinate's, so fewer and larger markers appear (KNOT-020e-fix). */}
              <View style={[styles.marker, { borderRadius: size / 2, height: size, width: size }]}>
                <Text style={[typeScale.caption, tabularNums, styles.markerCount]}>
                  {markerCountLabel(cluster.story_count)}
                </Text>
              </View>
            </PointAnnotation>
          );
        })}
      </MapView>

      {/* The header and the pillar tabs, floating over the map on one opaque
          canvas card: the title has to stay legible whatever the map draws
          beneath it, so the card is solid rather than translucent
          (KNOT-020g-fix9). The tabs keep the reference's underline treatment — the
          active tab pairs the primary ink with the indigo underline, so the row
          never depends on weight alone (KNOT-020e). */}
      <View style={[styles.bands, { paddingTop: topInset }]}>
        <Header title="Discover" />

        <View style={styles.tabs}>
          {FILTERS.map((option) => {
            const active = option.value === filter;
            return (
              <Pressable
                key={option.value}
                style={[styles.tab, active ? styles.tabActive : null]}
                onPress={() => setFilter(option.value)}
                accessibilityRole="button"
                accessibilityState={{ selected: active }}
                accessibilityLabel={option.label}
              >
                <Text
                  style={[typeScale.button, styles.tabLabel, active ? styles.tabLabelActive : null]}
                  numberOfLines={1}
                >
                  {option.label}
                </Text>
              </Pressable>
            );
          })}
        </View>
      </View>

      {/* The place list as a bottom sheet (KNOT-020e-fix): rounded top corners, the
          canvas surface, the design's upward shadow (elevation Level 3), a
          decorative drag handle, the aggregate totals, then the scrollable list.
          It is pinned to the bottom of the screen and overlaps the map's lower
          edge; the map keeps rendering behind it, and a marker beneath the sheet
          is covered rather than tappable. No drag gesture: the handle is
          decorative for the MVP. */}
      <View style={[styles.sheet, { height: sheetHeight }]}>
        <View style={styles.handle} />

        <View style={styles.sheetHeader}>
          <Text style={[typeScale.h2, styles.sheetTitle]}>Places</Text>
          <Text style={[typeScale.caption, styles.sheetSubtitle]}>
            {placeTotalsLabel(totals.places, totals.stories, totals.pillars)}
          </Text>
        </View>

        <FlatList
          style={styles.list}
          data={clusters}
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
                <View style={styles.placeText}>
                  <Text style={[typeScale.button, styles.placeName]} numberOfLines={1}>
                    {item.place}
                  </Text>
                  {subtitle === '' ? null : (
                    <Text style={[typeScale.caption, styles.placeMeta]} numberOfLines={1}>
                      {subtitle}
                    </Text>
                  )}
                </View>

                <View style={styles.placeCount}>
                  <Text style={[typeScale.bodyS, tabularNums, styles.placeCountText]}>
                    {storyCountLabel(item.story_count)}
                  </Text>
                  <CaretRight color={colors.text.tertiary} size={18} />
                </View>
              </Pressable>
            );
          }}
          ListEmptyComponent={
            !loading && error === undefined ? (
              <EmptyState
                illustrationName="no-stories"
                headline="No places yet."
                body="When people tell stories about a place, it appears on the map and in this list."
              />
            ) : null
          }
        />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // The header and the tabs as one floating card, pinned to the top of the screen
  // and above the map. `paddingTop` is set from the status bar's height at render
  // time, so the title clears the bar on a device that reports one and sits flush
  // where there is none (KNOT-020g-fix9).
  bands: {
    backgroundColor: colors.bg.canvas,
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
    zIndex: 2,
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
  // The decorative drag handle: a 32x4 pill in the hairline colour. No gesture
  // is attached — drag-to-expand is out of scope for the MVP (KNOT-020e-fix).
  handle: {
    alignSelf: 'center',
    backgroundColor: colors.border.hairline,
    borderRadius: radius.xs,
    height: 4,
    marginBottom: spacing.md,
    marginTop: spacing.sm,
    width: 32,
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
  marker: {
    alignItems: 'center',
    backgroundColor: colors.brand.primary,
    // A navy ring rather than the canvas white: on Mapbox's Light style a white
    // ring disappears into the pale basemap, so the marker's edge uses the
    // primary ink instead (KNOT-020e-fix2).
    borderColor: colors.text.primary,
    borderWidth: 2,
    justifyContent: 'center',
  },
  markerCount: {
    color: colors.text.onBrand,
  },
  placeCount: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.xs,
  },
  placeCountText: {
    color: colors.text.secondary,
  },
  placeMeta: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  placeName: {
    color: colors.text.primary,
  },
  placeRow: {
    alignItems: 'center',
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    flexDirection: 'row',
    gap: spacing.md,
    justifyContent: 'space-between',
    paddingHorizontal: layout.screenPaddingFeed,
    paddingVertical: spacing.md,
  },
  placeText: {
    flexShrink: 1,
  },
  // The sheet: rounded top corners, the canvas surface, and the design's upward
  // shadow (elevation Level 3, whose offset-Y is negative — exactly this use
  // case). Pinned to the bottom so it overlaps the map's lower edge. No
  // `overflow: hidden`, so the shadow is not clipped; the corners are above the
  // list, which never reaches them (KNOT-020e-fix). `zIndex` makes the stack
  // sheet (3) > floating header and tabs (2) > map (0) explicit, so the two bands
  // still order correctly on a screen short enough for them to meet
  // (KNOT-020g-fix9).
  sheet: {
    backgroundColor: colors.bg.canvas,
    borderTopLeftRadius: radius.xxl,
    borderTopRightRadius: radius.xxl,
    bottom: 0,
    left: 0,
    position: 'absolute',
    right: 0,
    zIndex: 3,
    ...elevation[3],
  },
  sheetHeader: {
    paddingBottom: spacing.md,
    paddingHorizontal: layout.screenPaddingFeed,
  },
  sheetSubtitle: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  sheetTitle: {
    color: colors.text.primary,
  },
  spinner: {
    marginTop: spacing.lg,
  },
  // One tab, as wide as its label: the reference draws the three filters
  // left-aligned with a 24-point gap, and underlines only the active one.
  tab: {
    borderBottomColor: 'transparent',
    borderBottomWidth: 2,
    paddingBottom: spacing.sm,
    paddingTop: spacing.md,
  },
  tabActive: {
    borderBottomColor: colors.brand.primary,
  },
  tabLabel: {
    color: colors.text.tertiary,
  },
  tabLabelActive: {
    color: colors.text.primary,
  },
  tabs: {
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    flexDirection: 'row',
    gap: spacing.xl,
    paddingHorizontal: layout.screenPaddingFeed,
  },
});
