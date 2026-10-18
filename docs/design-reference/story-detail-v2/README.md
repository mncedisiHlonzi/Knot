# Knot Story Detail: developer handoff

Open `index.html` for a gallery of all five screens in light and dark. Each screen is also a standalone file in `light/` and `dark/` (390 x 844 mobile viewport).

## Screens
1. `01-story-detail`: top of the screen, media carousel first.
2. `02-story-detail-scrolled`: story text, reaction bar, conversation and versions.
3. `03-media-viewer-image`: full-screen image viewer.
4. `04-media-viewer-video`: full-screen video viewer, playing.
5. `05-media-viewer-zoomed`: image zoomed, controls hidden.
The viewer is always black, so its files are the same in both themes.

## Story Detail layout (pt)
- Header 56 high: Back left, Save and More right. Touch targets 44.
- Media carousel: cards 334 x 320, radius 12, gap 8, left padding 16. The next card peeks about 32 pt to show there is more.
- Overlays on cards: counter pill "n / 10" top right (12/16, 600, scrim black 60%); video cards add a 56 pt play button (centre), duration chip bottom left, mute button 32 pt bottom right.
- Thumbnail strip: 44 x 44, radius 8, gap 6, padding 12 16, scrolls horizontally. Active item has a 2 pt indigo ring with 2 pt offset. Videos show a 16 pt play badge. Hide the strip when the story has one media item.
- Caption row: caption left (13/18), "Video / Photograph · duration" right (13/18, secondary).
- Content: 24 pt side padding. Overline 11/14 caps +0.08em, title 26/32 Plus Jakarta Sans 700, author row with Rooted badge, language switcher row (48 high, radius 12), reading text 17/28.
- Reaction bar: four equal columns, icon + label + count. Selected: tint fill, indigo icon.
- Bottom bar: "Tell this to your people", 48 high, radius 12, the only filled indigo element.

## Media rules
- 0 media: no carousel; start with the overline and title. 1 media: single full-width card, no strip. 2 to 10: carousel plus strip.
- Maximum 10 items, images and videos mixed, order set by the author.
- Videos never autoplay with sound. Pause when swiped away or when the viewer closes.
- Show a `sunken` placeholder (#F5F5F7 light, #1A1E29 dark) while loading; show a retry affordance on failure.
- Each item carries optional caption, required alt text for images (description for video), and credit. See `media-model.json` for a proposed shape.

## Viewer behaviour
- Tap a card or thumbnail to open at that item. Single tap toggles controls. Swipe sideways to change item, swipe down to dismiss.
- Images: pinch and double-tap to zoom; show the minimap and a zoom pill while zoomed.
- Video: scrubber with buffered and played segments, Mute, Subtitles, Back 10, Play/Pause (56 pt), Forward 10, Full screen. Subtitles sit above the controls.
- Counter "n / 10" and Share/More stay in the top bar.

## Accessibility
- Every icon-only control needs a label ("Pause", "Back 10 seconds", "Subtitles on").
- Announce position as "Photograph 3 of 10" or "Video 4 of 10, 48 seconds".
- Do not rely on colour alone for the active thumbnail: pair the ring with the counter.
- Honour Reduce Motion: replace slide transitions with a 120 ms crossfade.
- Respect text-size settings; captions reflow, they never truncate silently.

## Tokens
`tokens.json` has colours for both themes, type, spacing, radius, elevation and motion values.

## Notes
- These are static mockups with placeholder photos and sample text, not production code.
- Fonts (Inter, Plus Jakarta Sans) load from Google Fonts.
- Layout values above are taken from the mockups; confirm edge cases (very long captions, tall portrait video) in implementation.
