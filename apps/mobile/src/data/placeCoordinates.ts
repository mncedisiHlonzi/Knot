/**
 * A small, local lookup from a place name to a map coordinate.
 *
 * Knot deliberately stores no coordinates: a place is free text so that people
 * are not asked for anything more precise than a city or region, and the server
 * never geocodes (KNOT-ADR-020). The map therefore resolves a place to a point on
 * the device, using this hand-maintained table. A place that is not in the table
 * is not plotted; the discovery screen lists it below the map instead.
 *
 * Keys are the normalised place name (trimmed and lower-cased), matching the
 * server's `approximate_location_lower`. Matching is exact: "Cape Town" resolves,
 * "Cape Town, South Africa" does not, and the latter falls back to the list. This
 * is a deliberate MVP limit — fuzzy matching would be a guess.
 */

/** A latitude/longitude pair. */
export type PlaceCoordinate = {
  readonly latitude: number;
  readonly longitude: number;
};

/**
 * Well-known places and their coordinates, keyed by the normalised place name.
 *
 * The set favours places the Knot community is likely to tell stories about,
 * including a spread of South African cities alongside major world cities.
 */
const PLACE_COORDINATES: Readonly<Record<string, PlaceCoordinate>> = {
  // South Africa
  'cape town': { latitude: -33.9249, longitude: 18.4241 },
  johannesburg: { latitude: -26.2041, longitude: 28.0473 },
  durban: { latitude: -29.8587, longitude: 31.0218 },
  pretoria: { latitude: -25.7479, longitude: 28.2293 },
  'port elizabeth': { latitude: -33.9608, longitude: 25.6022 },
  bloemfontein: { latitude: -29.0852, longitude: 26.1596 },
  'east london': { latitude: -33.0292, longitude: 27.8546 },
  stellenbosch: { latitude: -33.9321, longitude: 18.8602 },
  soweto: { latitude: -26.2679, longitude: 27.8585 },
  polokwane: { latitude: -23.9045, longitude: 29.4689 },
  nelspruit: { latitude: -25.4753, longitude: 30.9694 },
  kimberley: { latitude: -28.7282, longitude: 24.7499 },
  // Africa
  nairobi: { latitude: -1.2921, longitude: 36.8219 },
  lagos: { latitude: 6.5244, longitude: 3.3792 },
  accra: { latitude: 5.6037, longitude: -0.187 },
  cairo: { latitude: 30.0444, longitude: 31.2357 },
  marrakech: { latitude: 31.6295, longitude: -7.9811 },
  kampala: { latitude: 0.3476, longitude: 32.5825 },
  'dar es salaam': { latitude: -6.7924, longitude: 39.2083 },
  windhoek: { latitude: -22.5609, longitude: 17.0658 },
  gaborone: { latitude: -24.6282, longitude: 25.9231 },
  harare: { latitude: -17.8252, longitude: 31.0335 },
  maputo: { latitude: -25.9692, longitude: 32.5732 },
  luanda: { latitude: -8.839, longitude: 13.2894 },
  addis: { latitude: 9.0301, longitude: 38.7406 },
  dakar: { latitude: 14.7167, longitude: -17.4677 },
  // Europe
  london: { latitude: 51.5074, longitude: -0.1278 },
  paris: { latitude: 48.8566, longitude: 2.3522 },
  berlin: { latitude: 52.52, longitude: 13.405 },
  madrid: { latitude: 40.4168, longitude: -3.7038 },
  lisbon: { latitude: 38.7223, longitude: -9.1393 },
  rome: { latitude: 41.9028, longitude: 12.4964 },
  amsterdam: { latitude: 52.3676, longitude: 4.9041 },
  dublin: { latitude: 53.3498, longitude: -6.2603 },
  stockholm: { latitude: 59.3293, longitude: 18.0686 },
  warsaw: { latitude: 52.2297, longitude: 21.0122 },
  athens: { latitude: 37.9838, longitude: 23.7275 },
  istanbul: { latitude: 41.0082, longitude: 28.9784 },
  moscow: { latitude: 55.7558, longitude: 37.6173 },
  // Middle East and Asia
  dubai: { latitude: 25.2048, longitude: 55.2708 },
  mumbai: { latitude: 19.076, longitude: 72.8777 },
  delhi: { latitude: 28.6139, longitude: 77.209 },
  bangalore: { latitude: 12.9716, longitude: 77.5946 },
  singapore: { latitude: 1.3521, longitude: 103.8198 },
  bangkok: { latitude: 13.7563, longitude: 100.5018 },
  jakarta: { latitude: -6.2088, longitude: 106.8456 },
  manila: { latitude: 14.5995, longitude: 120.9842 },
  tokyo: { latitude: 35.6762, longitude: 139.6503 },
  seoul: { latitude: 37.5665, longitude: 126.978 },
  beijing: { latitude: 39.9042, longitude: 116.4074 },
  shanghai: { latitude: 31.2304, longitude: 121.4737 },
  hongkong: { latitude: 22.3193, longitude: 114.1694 },
  'hong kong': { latitude: 22.3193, longitude: 114.1694 },
  // Americas
  'new york': { latitude: 40.7128, longitude: -74.006 },
  'los angeles': { latitude: 34.0522, longitude: -118.2437 },
  chicago: { latitude: 41.8781, longitude: -87.6298 },
  toronto: { latitude: 43.6532, longitude: -79.3832 },
  vancouver: { latitude: 49.2827, longitude: -123.1207 },
  'mexico city': { latitude: 19.4326, longitude: -99.1332 },
  bogota: { latitude: 4.711, longitude: -74.0721 },
  lima: { latitude: -12.0464, longitude: -77.0428 },
  'sao paulo': { latitude: -23.5505, longitude: -46.6333 },
  'rio de janeiro': { latitude: -22.9068, longitude: -43.1729 },
  'buenos aires': { latitude: -34.6037, longitude: -58.3816 },
  // Oceania
  sydney: { latitude: -33.8688, longitude: 151.2093 },
  melbourne: { latitude: -37.8136, longitude: 144.9631 },
  auckland: { latitude: -36.8485, longitude: 174.7633 },
};

/**
 * Returns the coordinate for a place, or undefined when the place is not in the
 * table. The comparison trims and lower-cases, mirroring the server's normalised
 * place matching.
 */
export function coordinatesForPlace(place: string): PlaceCoordinate | undefined {
  return PLACE_COORDINATES[place.trim().toLowerCase()];
}
