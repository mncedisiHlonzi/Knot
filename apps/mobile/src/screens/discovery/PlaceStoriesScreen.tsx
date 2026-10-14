import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';
import Mapbox, { Camera, MapView, PointAnnotation } from '@rnmapbox/maps';

import { describeError } from '../../api/client';
import { PLACE_PAGE_SIZE, discoveryApi } from '../../api/discovery';
import { Story } from '../../api/stories';
import EmptyState from '../../components/EmptyState';
import FeedEntry from '../../components/FeedEntry';
import Header from '../../components/Header';
import { KNOT_MAPBOX_TOKEN } from '../../config/dev';
import { colors, layout, spacing, typeScale } from '../../theme';
import {
  PILLAR_TABS,
  type PillarFilter,
  pillarEmptyHeadline,
  placeMapPoint,
  placeSummaryLabel,
} from './discoveryFormat';

/**
 * The compact map strip's height, in points.
 *
 * The reference draws a 96-point strip; this is taller so Mapbox's mandated logo
 * and attribution, which sit inside the strip, do not crowd it — the follow-up the
 * KNOT-020e-fix3 note proposed. The strip is a plain normal-flow box of exactly
 * this height and clips its map, so nothing it holds can draw over the list below
 * (KNOT-020e-fix5).
 */
const STRIP_HEIGHT = 140;

/**
 * City-level zoom, so the strip reads as "here is the town" rather than a country
 * or a single street.
 */
const STRIP_ZOOM = 11;

/**
 * The strip's marker: the Discovery Map's marker in miniature — an indigo dot
 * ringed in the navy ink, with no story count to show.
 */
const STRIP_MARKER_SIZE = 14;
const STRIP_MARKER_BORDER = 2;

// Mapbox needs its access token before any map mounts. Setting it once, at module
// load, keeps it out of the component and off every render. The token is public
// (see `config/dev.ts`); setting it twice, as DiscoveryMapScreen also does, is
// idempotent.
void Mapbox.setAccessToken(KNOT_MAPBOX_TOKEN);

type PlaceStoriesScreenProps = {
  /** The place whose stories are shown, as it was named by the server. */
  readonly place: string;
  /**
   * The country the place's cluster was keyed by, or null when the cluster's
   * stories record none. It narrows every page to that cluster's own stories, so
   * the list below matches the `storyCount` in the header. Left undefined, the
   * page lists every story at the place name whatever country each records
   * (KNOT-020e-fix2).
   */
  readonly country?: string | null;
  /**
   * The place's coordinate, averaged across its cluster's stories, or null when
   * the cluster has none — a place told only by name has no point to plot. Both
   * are handed over from the cluster, so the strip costs no extra request; when
   * either is null the strip is not rendered at all (KNOT-020e-fix3).
   */
  readonly latitude?: number | null;
  readonly longitude?: number | null;
  /**
   * How many stories the place holds, and how they split across the two pillars.
   *
   * Both come from the place's cluster on the Discovery Map, which is where this
   * screen is opened from. The place endpoint pages stories and carries no totals
   * of its own, so the cluster is handed over with the place rather than fetched
   * again — the header is exact and costs no extra request (KNOT-020e).
   */
  readonly storyCount: number;
  readonly pillarCounts: Readonly<Record<string, number>>;
  /**
   * The signed-in user's access token, or undefined when signed out. It is sent
   * on every page request so the server hides blocked authors' stories; an
   * anonymous request sees everything.
   */
  readonly token?: string;
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /** Called when the person leaves this place. */
  readonly onBack: () => void;
};

/**
 * Every story told at one place, newest first.
 *
 * The place is narrowed to the country its cluster was keyed by, so the list
 * matches the header's story count; a cluster with no country asks for the
 * stories that record none (KNOT-020e-fix2).
 *
 * Paging is the same keyset cursor the feed uses: each page returns an opaque
 * `next_cursor` sent back verbatim for the following page, and an empty cursor
 * means the end. Reaching the end of the list asks for the next page;
 * pull-to-refresh starts again from the newest story. A place with no stories is a
 * normal empty state, not an error — the server answers 200 with an empty list.
 *
 * The design is the reference's (KNOT-020e, KNOT-020e-fix3, KNOT-020e-fix6): the
 * place is the screen's headline with its totals beneath it, a compact
 * **non-interactive map strip** pinning where the place is, the **pillar tabs**
 * (All / Heritage / Wonder), and the stories below as the same `FeedEntry` the
 * Feed renders — carrying the same author, media, and count enrichment the feed
 * does (KNOT-020e-fix2) — so a story reads the same in both places. The strip is
 * drawn only when the cluster carried a coordinate. A tab is a **server-side**
 * filter rather than a view of the loaded page, so switching tabs starts the list
 * again from the first page. Nothing here auto-plays: the Feed is the only screen
 * that does (KNOT-ADR-068), so a video entry renders on its paused frame.
 */
export default function PlaceStoriesScreen({
  place,
  country,
  latitude,
  longitude,
  storyCount,
  pillarCounts,
  token,
  onOpenStory,
  onBack,
}: PlaceStoriesScreenProps): React.ReactElement {
  const [stories, setStories] = useState<readonly Story[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  // The active pillar tab. The choice is a server-side query rather than a view of
  // the loaded page, so changing it starts the list again from the first page
  // (KNOT-020e-fix6).
  const [pillar, setPillar] = useState<PillarFilter>('all');

  const loadFirstPage = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'initial') {
        setLoading(true);
        // A new pillar is a different query, so the page starts over: the previous
        // tab's rows must not linger under the new tab's label.
        setStories([]);
        setNextCursor('');
      } else {
        setRefreshing(true);
      }
      setError(undefined);

      try {
        const page = await discoveryApi.listStoriesAtPlace(
          place,
          { country, limit: PLACE_PAGE_SIZE, pillar: pillar === 'all' ? undefined : pillar },
          token,
        );
        setStories(page.stories);
        setNextCursor(page.next_cursor);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [country, pillar, place, token],
  );

  useEffect(() => {
    void loadFirstPage('initial');
  }, [loadFirstPage]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await discoveryApi.listStoriesAtPlace(
        place,
        {
          country,
          cursor: nextCursor,
          limit: PLACE_PAGE_SIZE,
          pillar: pillar === 'all' ? undefined : pillar,
        },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a story already on screen.
      setStories((existing) => [...existing, ...page.stories]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [country, loadingMore, nextCursor, pillar, place, token]);

  // The strip is drawn only when the cluster carried a usable coordinate; a place
  // told only by name has none, so the header then sits straight on the list with
  // no gap. The guard also rejects a non-finite or out-of-range pair.
  const mapPoint = placeMapPoint(latitude, longitude);

  return (
    <View style={styles.container}>
      <Header
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Back"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      {/* The reference's place block: the place as the screen's headline, with its
          story and pillar totals as the quiet line beneath it. */}
      <View style={styles.heading}>
        <Text style={[typeScale.h1, styles.place]} numberOfLines={2}>
          {place}
        </Text>
        <Text style={[typeScale.caption, styles.summary]}>
          {placeSummaryLabel(storyCount, pillarCounts)}
        </Text>
      </View>

      {/* The reference's compact map strip (KNOT-020e-fix3): a decorative preview
          of where the place is. It is a sibling of the list rather than a list
          header, so it stays put while the stories scroll beneath it, and it is
          fully non-interactive — no pan, zoom, rotate, or pitch. The wrapper
          clips its map, so the map can never draw over the stories
          (KNOT-020e-fix5). */}
      {mapPoint === undefined ? null : (
        <View style={styles.strip}>
          {/* A `TextureView`, not the Android default `GLSurfaceView`: a surface
              view is punched through the RN view hierarchy, so it ignores this
              wrapper's clipping and paints over the tab row and the list whatever
              the z-order says. A texture view is an ordinary view, clipped and
              stacked with everything else (KNOT-020e-fix6). */}
          <MapView
            style={styles.stripMap}
            styleURL={Mapbox.StyleURL.Light}
            surfaceView={false}
            scrollEnabled={false}
            zoomEnabled={false}
            rotateEnabled={false}
            pitchEnabled={false}
          >
            <Camera
              defaultSettings={{
                centerCoordinate: [mapPoint.longitude, mapPoint.latitude],
                zoomLevel: STRIP_ZOOM,
              }}
            />
            {/* PointAnnotation renders its child as the marker. The strip has no
                count to show, so it is the Discovery Map's marker stripped to
                its indigo fill and navy ring. */}
            <PointAnnotation
              id="place-strip-marker"
              coordinate={[mapPoint.longitude, mapPoint.latitude]}
            >
              <View style={styles.stripMarker} />
            </PointAnnotation>
          </MapView>
        </View>
      )}

      {/* The pillar filter, mirroring the Feed's tab row: every tab carries the
          button weight, and the active one is marked by ink and the indigo
          underline rather than by weight, so the row never depends on weight
          alone. The choice is a server-side filter, so tapping a tab refetches
          from the first page (KNOT-020e-fix6). */}
      <View style={styles.tabs}>
        {PILLAR_TABS.map((option) => {
          const active = option.value === pillar;
          return (
            <Pressable
              key={option.value}
              style={[styles.tab, active ? styles.tabActive : null]}
              onPress={() => setPillar(option.value)}
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

      <FlatList
        style={styles.list}
        data={stories}
        keyExtractor={(story) => story.id}
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              void loadFirstPage('refresh');
            }}
            tintColor={colors.text.secondary}
          />
        }
        onEndReached={() => {
          void loadMore();
        }}
        onEndReachedThreshold={0.5}
        renderItem={({ item }) => (
          <FeedEntry
            story={{
              author_avatar_url: item.author_avatar_url,
              author_display_name: item.author_display_name,
              author_rooted: item.author_rooted ?? null,
              comment_count: item.comment_count,
              created_at: item.created_at,
              first_media_type: item.first_media_type ?? null,
              first_media_url: item.first_media_url ?? null,
              id: item.id,
              language: item.language,
              language_count: item.language_count,
              media_count: item.media_count,
              pillar: item.pillar,
              place: item.approximate_location,
              preview: item.body,
              reactions: item.reactions,
              title: item.title,
              version_count: item.version_count,
            }}
            onPress={() => onOpenStory(item.id)}
          />
        )}
        ListEmptyComponent={
          loading ? (
            <ActivityIndicator style={styles.spinner} />
          ) : error !== undefined ? (
            <Text style={styles.error}>{error}</Text>
          ) : (
            <EmptyState
              illustrationName="no-stories"
              headline={pillarEmptyHeadline(pillar)}
              body={pillar === 'all' ? `Be the first to tell one about ${place}.` : undefined}
            />
          )
        }
        ListFooterComponent={
          loadingMore ? (
            <ActivityIndicator style={styles.footerSpinner} />
          ) : error !== undefined && stories.length > 0 ? (
            <Text style={styles.error}>{error}</Text>
          ) : null
        }
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  // The entries are edge-to-edge and carry their own horizontal inset
  // (`FeedEntry` uses `layout.screenPaddingFeed`), so the list adds none.
  content: {
    paddingBottom: spacing.xxxl,
  },
  error: {
    color: colors.state.danger,
    paddingHorizontal: layout.screenPaddingFeed,
    paddingVertical: spacing.md,
    textAlign: 'center',
  },
  footerSpinner: {
    marginVertical: spacing.xl,
  },
  heading: {
    paddingBottom: spacing.md,
    paddingHorizontal: layout.screenPaddingFeed,
    paddingTop: spacing.xs,
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  // The list takes whatever the header, the place block, the map strip, and the
  // tab row leave, so nothing pushes it off screen and it scrolls in the
  // remainder. The explicit background and z-index keep a scrolled story beneath
  // the bands above it rather than drawn across them (KNOT-020e-fix5,
  // KNOT-020e-fix6).
  list: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
    zIndex: 0,
  },
  place: {
    color: colors.text.primary,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  // The compact map strip: a normal-flow box of a fixed height, fully outside the
  // list so it never scrolls with the stories. `overflow: 'hidden'` is
  // load-bearing rather than cosmetic — the MapView is a native view and React
  // Native does not clip by default, so without it the map lays out and draws
  // beyond these bounds and paints over the first stories. The canvas fill makes
  // the box opaque even before the map has drawn, so nothing behind it shows
  // through (KNOT-020e-fix5).
  strip: {
    backgroundColor: colors.bg.canvas,
    height: STRIP_HEIGHT,
    overflow: 'hidden',
    width: '100%',
    // The strip is the topmost band, so a story scrolled under it is hidden rather
    // than drawn across it. The stacking is 2 (strip) > 1 (tabs) > 0 (list)
    // (KNOT-020e-fix6).
    zIndex: 2,
  },
  // The map gets explicit dimensions rather than `flex: 1`: a native map view is
  // not a plain Yoga-measured View, and sizing it by flex alone is what let it lay
  // out larger than the strip it sits in (KNOT-020e-fix5).
  stripMap: {
    height: '100%',
    width: '100%',
  },
  // The Discovery Map's marker in miniature: indigo fill, navy ring, no count.
  stripMarker: {
    backgroundColor: colors.brand.primary,
    borderColor: colors.text.primary,
    borderRadius: STRIP_MARKER_SIZE / 2,
    borderWidth: STRIP_MARKER_BORDER,
    height: STRIP_MARKER_SIZE,
    width: STRIP_MARKER_SIZE,
  },
  summary: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  // The pillar tabs mirror the Feed's: three equal-width tabs, each carrying the
  // row's hairline under it and the active one drawn 2px in the brand colour. The
  // row is opaque and stacked above the list, so a scrolled story passes beneath
  // it rather than through it (KNOT-020e-fix6).
  tab: {
    alignItems: 'center',
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    flex: 1,
    paddingBottom: spacing.sm,
    paddingTop: spacing.md,
  },
  tabActive: {
    borderBottomColor: colors.brand.primary,
    borderBottomWidth: 2,
  },
  tabLabel: {
    color: colors.text.tertiary,
    textAlign: 'center',
  },
  tabLabelActive: {
    color: colors.text.primary,
  },
  tabs: {
    backgroundColor: colors.bg.canvas,
    flexDirection: 'row',
    paddingHorizontal: spacing.lg,
    zIndex: 1,
  },
});
