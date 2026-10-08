import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import MapView, { Marker, UrlTile } from 'react-native-maps';

import { describeError } from '../../api/client';
import { PlaceCluster, discoveryApi } from '../../api/discovery';
import { Pillar } from '../../api/stories';
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

/** A world-scale starting region, so every plotted place is on screen. */
const INITIAL_REGION = {
  latitude: 10,
  longitude: 10,
  latitudeDelta: 120,
  longitudeDelta: 120,
};

/**
 * Renders a story count with the right singular/plural noun.
 */
function storyLabel(count: number): string {
  return count === 1 ? '1 story' : `${count} stories`;
}

/**
 * Discovery, as a map.
 *
 * The server groups stories by place but stores no coordinates, so this screen
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

      <MapView style={styles.map} initialRegion={INITIAL_REGION}>
        {/*
         * Use OpenStreetMap raster tiles instead of the platform's default
         * basemap. The default on Android is the Google Maps SDK, which needs a
         * Google Cloud project with a billing method and an API key; OSM needs
         * neither, which is what makes the map renderable for free at MVP scale.
         *
         * Migration path: when Knot outgrows OSM's usage policy, swap this one
         * component for the chosen provider's tile layer (Google Maps with a key,
         * or Mapbox). MapView and Marker are unchanged, so the rest of this
         * screen does not move. See KNOT-ADR-023.
         */}
        <UrlTile urlTemplate="https://tile.openstreetmap.org/{z}/{x}/{y}.png" maximumZ={19} />
        {plotted.map((cluster) => {
          const coordinate = coordinatesForPlace(cluster.place);
          if (coordinate === undefined) {
            return null;
          }
          return (
            <Marker
              key={cluster.place}
              coordinate={coordinate}
              title={cluster.place}
              description={storyLabel(cluster.story_count)}
              onPress={() => onOpenPlace(cluster.place)}
            />
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
