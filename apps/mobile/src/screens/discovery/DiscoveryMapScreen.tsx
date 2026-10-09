import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import Mapbox, { Camera, MapView, PointAnnotation } from '@rnmapbox/maps';

import { describeError } from '../../api/client';
import { PlaceCluster, discoveryApi } from '../../api/discovery';
import { Pillar } from '../../api/stories';
import { KNOT_MAPBOX_TOKEN } from '../../config/dev';
import { coordinatesForPlace } from '../../data/placeCoordinates';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type DiscoveryMapScreenProps = {
  /** Called when a place is tapped, on the map or in the list below it. */
  readonly onOpenPlace: (place: string) => void;
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

// Mapbox needs its access token before any map mounts. Setting it once, at module
// load, keeps it out of the component and off every render. The token is public
// (see `config/dev.ts`).
void Mapbox.setAccessToken(KNOT_MAPBOX_TOKEN);

/**
 * Renders a story count with the right singular/plural noun.
 */
function storyLabel(count: number): string {
  return count === 1 ? '1 story' : `${count} stories`;
}

/**
 * Discovery, as a map.
 *
 * The map is rendered natively with Mapbox (`@rnmapbox/maps`, KNOT-ADR-026). The
 * server groups stories by place but stores no coordinates, so this screen
 * resolves each place to a point with a local lookup table
 * (`src/data/placeCoordinates.ts`). A place that is not in the table is not
 * plotted; it is listed below the map instead, so nothing is hidden.
 *
 * Tapping a marker, or an entry in the list, opens that place's stories. There is
 * no user location and no permission prompt: the map is a way in to places, not a
 * way to find the user.
 */
export default function DiscoveryMapScreen({
  onOpenPlace,
}: DiscoveryMapScreenProps): React.ReactElement {
  const [clusters, setClusters] = useState<readonly PlaceCluster[]>([]);
  const [filter, setFilter] = useState<PillarFilter>('all');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = await discoveryApi.listClusters(filter === 'all' ? {} : { pillar: filter });
      setClusters(result.clusters);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    void load();
  }, [load]);

  const plotted = clusters.filter((cluster) => coordinatesForPlace(cluster.place) !== undefined);

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.title}>Explore by place</Text>
        <Text style={styles.subtitle}>Stories told around the world.</Text>

        <View style={styles.filters}>
          {FILTERS.map((option) => {
            const active = option.value === filter;
            return (
              <Pressable
                key={option.value}
                style={[styles.filter, active ? styles.filterActive : null]}
                onPress={() => setFilter(option.value)}
                accessibilityRole="button"
                accessibilityState={{ selected: active }}
              >
                <Text style={[styles.filterText, active ? styles.filterTextActive : null]}>
                  {option.label}
                </Text>
              </Pressable>
            );
          })}
        </View>
      </View>

      {/*
       * The map is drawn natively with Mapbox (`@rnmapbox/maps`), superseding
       * react-native-maps + OpenStreetMap (KNOT-ADR-026). OSM's tile server
       * returns an `x-blocked` header to anonymous clients and react-native-maps
       * cannot send identifying headers, so its tiles never rendered; Mapbox draws
       * tiles from its own native SDK using an access token.
       *
       * The token is the *public* one from `src/config/dev.ts`. The Dark style is
       * used so the map sits on the app's navy canvas, and Mapbox's logo and
       * attribution are left enabled (required by Mapbox's terms).
       */}
      <MapView style={styles.map} styleURL={Mapbox.StyleURL.Dark}>
        <Camera defaultSettings={{ centerCoordinate: INITIAL_CENTER, zoomLevel: INITIAL_ZOOM }} />
        {plotted.map((cluster) => {
          const coordinate = coordinatesForPlace(cluster.place);
          if (coordinate === undefined) {
            return null;
          }
          return (
            <PointAnnotation
              key={cluster.place}
              id={cluster.place}
              coordinate={[coordinate.longitude, coordinate.latitude]}
              title={cluster.place}
              onSelected={() => onOpenPlace(cluster.place)}
            >
              {/* PointAnnotation renders its child as the marker, so this is the
                  pin: a brand-purple dot ringed in white to read on the dark map. */}
              <View style={styles.marker} />
            </PointAnnotation>
          );
        })}
      </MapView>

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <FlatList
        data={clusters}
        keyExtractor={(cluster) => cluster.place}
        contentContainerStyle={styles.listContent}
        ListHeaderComponent={
          <View>
            <Text style={styles.sectionTitle}>Places</Text>
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          </View>
        }
        renderItem={({ item }) => (
          <Pressable style={styles.placeRow} onPress={() => onOpenPlace(item.place)}>
            <Text style={styles.placeName}>{item.place}</Text>
            <Text style={styles.placeMeta}>
              {storyLabel(item.story_count)}
              {coordinatesForPlace(item.place) === undefined ? ' · not on map' : ''}
            </Text>
          </Pressable>
        )}
        ListEmptyComponent={
          !loading && error === undefined ? (
            <Text style={styles.empty}>No places yet. Be the first to tell one.</Text>
          ) : null
        }
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  empty: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.md,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.sm,
  },
  filter: {
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.xs,
  },
  filterActive: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.brand.purple,
  },
  filterText: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  filterTextActive: {
    color: colors.text.primary,
  },
  filters: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  header: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
  },
  listContent: {
    paddingBottom: spacing['3xl'],
    paddingHorizontal: spacing.xl,
  },
  map: {
    height: 280,
    marginTop: spacing.lg,
    width: '100%',
  },
  marker: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.text.primary,
    borderRadius: radius.pill,
    borderWidth: 2,
    height: 18,
    width: 18,
  },
  placeMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  placeName: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  placeRow: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    padding: spacing.lg,
  },
  sectionTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
    marginTop: spacing.xl,
  },
  spinner: {
    marginTop: spacing.lg,
  },
  subtitle: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.xs,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});
