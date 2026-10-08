/**
 * The backend URL for local development.
 *
 * The Knot API runs on the developer's own machine on port 8080, but the host
 * the app must dial depends on where the app is running:
 *
 *   - iOS Simulator     → http://localhost:8080    (shares the Mac's loopback)
 *   - Android emulator  → http://10.0.2.2:8080      (the emulator's host-loopback alias)
 *   - Physical device   → the Mac's Wi-Fi address   (e.g. http://10.27.80.173:8080)
 *
 * The value below is the founder's Mac Wi-Fi address at the time of writing. It
 * is not a secret and not production configuration — it is a per-developer,
 * per-network convenience default, tied to one machine on one Wi-Fi. A physical
 * phone can only reach the Mac over the LAN, and `localhost` on the phone means
 * the phone itself, so the LAN address is the only thing that works there.
 *
 * Because that address changes whenever the Mac joins a different network, this
 * file is the single place to update: change the constant and reload the app.
 * The phone and the Mac must be on the same Wi-Fi network.
 *
 * To read the Mac's current Wi-Fi address:  ipconfig getifaddr en0
 */
export const DEV_API_URL = 'http://10.27.80.173:8080';
