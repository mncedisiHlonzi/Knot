import React, { useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { KNOT_MAPBOX_TOKEN } from '../config/dev';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

/** A place the user chose from the geocoding suggestions. */
export type PickedPlace = {
  /** The place's primary name, e.g. "Manguzi". */
  readonly place: string;
  readonly latitude: number;
  readonly longitude: number;
  /** The country the geocoder reported, or null when it named none. */
  readonly country: string | null;
};

/** A suggestion, which is a PickedPlace plus the context line to display. */
type GeocodeResult = PickedPlace & {
  /** The region/country line under the name, e.g. "KwaZulu-Natal, South Africa". */
  readonly contextLine: string;
};

type LocationPickerProps = {
  /** The text currently in the field (controlled by the parent). */
  readonly text: string;
  /** The chosen place, or null when nothing has been selected. */
  readonly selected: PickedPlace | null;
  readonly onChangeText: (text: string) => void;
  readonly onSelect: (place: PickedPlace) => void;
  /** Clears both the text and the selection. */
  readonly onClear: () => void;
  readonly placeholder?: string;
};

/** How long to wait after the last keystroke before querying the geocoder. */
const DEBOUNCE_MS = 300;
/** The shortest query that is worth sending. */
const MIN_QUERY_LENGTH = 2;
/** How many suggestions to ask for. */
const RESULT_LIMIT = 5;
/** The place types the picker offers: settlements and regions, not addresses. */
const GEOCODE_TYPES = 'place,locality,region';

/**
 * A session-only cache of geocoding results, keyed by the query. It is an
 * in-memory Map that dies with the app: nothing is persisted, so a change to the
 * geocoder's data is picked up on the next launch, and no storage permission is
 * needed.
 */
const geocodeCache = new Map<string, readonly GeocodeResult[]>();

/** The slice of the Mapbox Geocoding response this picker reads. */
type MapboxContextEntry = { readonly id?: string; readonly text?: string };
type MapboxFeature = {
  readonly id: string;
  readonly text?: string;
  readonly place_name?: string;
  readonly center?: readonly [number, number];
  readonly context?: readonly MapboxContextEntry[];
};
type GeocodeResponse = { readonly features?: readonly MapboxFeature[] };

/** Reads the country name out of a feature's context array, or null. */
function countryFromContext(context: readonly MapboxContextEntry[] | undefined): string | null {
  const country = context?.find((entry) => entry.id?.startsWith('country.'));
  return country?.text ?? null;
}

/** The context line shown under a suggestion: the place_name minus the name. */
function contextLineFor(feature: MapboxFeature): string {
  const primary = feature.text ?? '';
  const full = feature.place_name ?? '';
  if (primary !== '' && full.toLowerCase().startsWith(primary.toLowerCase())) {
    return full.slice(primary.length).replace(/^,?\s*/, '');
  }
  return full;
}

/** Turns a Mapbox feature into a suggestion, or null when it has no coordinate. */
function toResult(feature: MapboxFeature): GeocodeResult | null {
  if (feature.center === undefined || feature.center.length !== 2) {
    return null;
  }
  const [longitude, latitude] = feature.center;
  if (feature.text === undefined || feature.text === '') {
    return null;
  }

  return {
    place: feature.text,
    latitude,
    longitude,
    country: countryFromContext(feature.context),
    contextLine: contextLineFor(feature),
  };
}

/**
 * Queries the Mapbox Geocoding API for a place. The request is sent straight from
 * the app using the public token from `config/dev.ts`; the backend never sees it
 * and needs no Mapbox credential.
 */
async function searchPlaces(query: string, signal: AbortSignal): Promise<readonly GeocodeResult[]> {
  const url =
    `https://api.mapbox.com/geocoding/v5/mapbox.places/${encodeURIComponent(query)}.json` +
    `?access_token=${encodeURIComponent(KNOT_MAPBOX_TOKEN)}&limit=${RESULT_LIMIT}&types=${GEOCODE_TYPES}`;

  const response = await fetch(url, { signal });
  if (!response.ok) {
    throw new Error(`geocoder responded ${response.status}`);
  }

  const body = (await response.json()) as GeocodeResponse;
  const results: GeocodeResult[] = [];
  for (const feature of body.features ?? []) {
    const result = toResult(feature);
    if (result !== null) {
      results.push(result);
    }
  }

  return results;
}

/**
 * A geocoding-aware place field.
 *
 * As the user types, the field asks Mapbox for matching places and lists them.
 * Selecting one reports the place name and its coordinate to the parent, so a
 * story (or a Rooted signal) can store a point the map can plot
 * (KNOT-ADR-034, KNOT-ADR-035). Free text is allowed while typing, but only a
 * selection carries a coordinate — the parent decides whether to require one
 * (KNOT-ADR-036).
 *
 * The input is debounced, needs at least two characters, is cached in memory for
 * the session, and treats any geocoder failure as a quiet "Could not search"
 * rather than an error the app cannot recover from.
 */
export default function LocationPicker({
  text,
  selected,
  onChangeText,
  onSelect,
  onClear,
  placeholder,
}: LocationPickerProps): React.ReactElement {
  const [results, setResults] = useState<readonly GeocodeResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    const query = text.trim();

    if (query.length < MIN_QUERY_LENGTH) {
      setResults([]);
      setSearching(false);
      setError(undefined);
      return;
    }

    const cached = geocodeCache.get(query);
    if (cached !== undefined) {
      setResults(cached);
      setSearching(false);
      setError(undefined);
      return;
    }

    const controller = new AbortController();
    setSearching(true);

    const timer = setTimeout(() => {
      searchPlaces(query, controller.signal)
        .then((found) => {
          geocodeCache.set(query, found);
          setResults(found);
          setError(undefined);
        })
        .catch(() => {
          if (controller.signal.aborted) {
            return;
          }
          setResults([]);
          setError('Could not search');
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setSearching(false);
          }
        });
    }, DEBOUNCE_MS);

    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [text]);

  const showResults = selected === null && results.length > 0;

  return (
    <View>
      <View style={styles.inputRow}>
        <TextInput
          style={styles.input}
          value={text}
          onChangeText={onChangeText}
          placeholder={placeholder ?? 'Search for a place'}
          placeholderTextColor={colors.text.secondary}
          autoCorrect={false}
          autoCapitalize="words"
        />
        {text !== '' || selected !== null ? (
          <Pressable
            style={styles.clear}
            onPress={onClear}
            accessibilityRole="button"
            accessibilityLabel="Clear place"
          >
            <Text style={styles.clearText}>Clear</Text>
          </Pressable>
        ) : null}
      </View>

      {selected !== null ? (
        <Text style={styles.selected}>
          {`Selected: ${selected.place}${selected.country === null ? '' : `, ${selected.country}`}`}
        </Text>
      ) : null}

      {searching ? (
        <ActivityIndicator style={styles.spinner} color={colors.text.secondary} />
      ) : null}
      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      {showResults ? (
        <View style={styles.results}>
          {results.map((result) => (
            <Pressable
              key={`${result.place}-${result.latitude}-${result.longitude}`}
              style={styles.result}
              onPress={() => onSelect(result)}
              accessibilityRole="button"
            >
              <Text style={styles.resultName}>{result.place}</Text>
              {result.contextLine !== '' ? (
                <Text style={styles.resultContext}>{result.contextLine}</Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  clear: {
    marginLeft: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  clearText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    flex: 1,
    fontSize: fontSizes.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  inputRow: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  result: {
    borderTopColor: colors.border.subtle,
    borderTopWidth: 1,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  resultContext: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: 2,
  },
  resultName: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  results: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.xs,
    overflow: 'hidden',
  },
  selected: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  spinner: {
    marginTop: spacing.sm,
  },
});
