# Knot — Brand

**Status: specification.** This is the canonical brand reference for Knot. It captures the
identity from the founder's brand reference material so that every screen and every future
asset is drawn from one source rather than re-decided per screen.

## Overview

Knot ties human stories, languages, and truths together. The brand is built to feel **warm,
human, and clear**: a network of people exchanging knowledge across languages, not a
broadcast feed and not a machine-translation layer. Visually that means an open, clean
canvas (white space, one strong type family), a confident purple core that signals trust
and creativity, and accents of magenta and blue for energy, connection, and growth. Nothing
is decorative for its own sake — colour, type, and space exist to keep a story legible and
its people visible.

## Logo assets

Three marks make up the identity. Use the one that matches the space you have.

| Asset | What it is | Use it when |
| --- | --- | --- |
| **Primary Logo (horizontal)** | The logomark and the wordmark locked up side by side. | The default. Headers, splash and launch screens, document covers, anywhere there is horizontal room. |
| **Logomark (standalone)** | The symbol on its own, no word. | App icons, avatars, favicons, tight square spaces, and sizes where the wordmark would be illegible. |
| **Wordmark** | The name "Knot" set in the brand type, no symbol. | Where the mark is already present, or the name must read as text — alongside a partner logo, in a footer, in running copy. |

Prefer the horizontal primary logo. Reach for the logomark only when the space is square or
small, and for the wordmark only when the symbol would be redundant.

## Logo variations

Four colour variations cover every surface. Choose by **background**, not by preference.

| Variation | Use it when |
| --- | --- |
| **Primary Light** | The default on light backgrounds. Full-colour mark on white or near-white. |
| **Primary Dark** | On dark backgrounds (Navy `#0F172A` or darker); the mark lightens so it stays legible. |
| **Monochrome Light** | Single-colour contexts on a **light** background: system surfaces, one-colour print, anywhere full colour is unavailable. |
| **Monochrome Dark** | The same, on a **dark** background. |

"Light" and "Dark" name the **background the variation is drawn for**. If the background is
light, use a Light variation; if dark, a Dark one. When colour is unavailable entirely,
use a Monochrome variation rather than a desaturated full-colour mark.

## App icons

Four icon treatments ship: **Color**, **Dark**, **Light**, and **Monochrome**.

- **Color** — the primary app icon, and the default on both platforms.
- **Dark** / **Light** — the themed variants, chosen when the device or launcher asks for a
  dark or light icon.
- **Monochrome** — the single-colour variant for themed-icon systems that tint the icon
  themselves.

Map these onto the platform's adaptive-icon mechanism at the level of "provide composition
and colour variants, and let the platform choose". The exact asset sizes and the build
tooling that generates the icon set are an implementation concern for the task that
introduces them, not a brand decision. Until then, treat these four as the specification of
what must exist, not as committed files (see the asset source note).

## Colour palette

Six colours. Each carries one idea, and the idea is why it exists.

| Colour | Hex | Meaning |
| --- | --- | --- |
| **Purple** | `#7B3FE4` | Primary brand. Trust, creativity, innovation. |
| **Magenta** | `#E83EBC` | Secondary brand. Energy, creativity, connection. |
| **Blue** | `#3B82F6` | Secondary brand. Communication, growth. |
| **Navy** | `#0F172A` | Text, headers, key UI. Clarity, focus, stability. |
| **Gray** | `#94A3B8` | Secondary text, borders. Balance, simplicity. |
| **White** | `#FFFFFF` | Backgrounds. Clean space. |

Purple leads; magenta and blue support it; navy and gray carry the text and structure; white
is the canvas. These six are the **brand anchors** — the solid colours you paint with.

### Brand anchors and gradient endpoints

The reference material contains two closely related sets of values, and both are correct
because they do different jobs:

- **Brand anchors** — the solid colours above (`#7B3FE4`, `#E83EBC`, `#3B82F6`). Fills, text,
  icons, borders: anywhere a single flat colour is wanted.
- **Gradient endpoints** — the stop colours in the gradients below (`#6B46FF`, `#004BFF`,
  `#FF3E85`, `#FF3E63`, `#FF8A3D`). Only ever the ends of a gradient.

They are not interchangeable. Do not substitute a brand anchor into a gradient, or a
gradient endpoint into a flat fill, even when the two sit close together.

## Gradients

Three gradients, each with a job.

| Gradient | From → To | Use it when |
| --- | --- | --- |
| **Primary** | Purple `#6B46FF` → Blue `#004BFF` | The default brand surface: hero areas, the primary action, splash and launch treatment. |
| **Accent** | Magenta `#FF3E85` → Purple `#6B46FF` | A moment of energy: highlights, celebratory states, a connection made. |
| **Warm** | Orange `#FF8A3D` → Pink `#FF3E63` | Warmth and human closeness: heritage, place, and person-led moments. |

The palette's `#7B3FE4` and `#3B82F6` remain the solid-colour **anchors**; the Primary
gradient's `#6B46FF` and `#004BFF` are its **endpoints**. A gradient is a surface, not a
text style — keep type off a gradient unless contrast has been checked, and when in doubt lay
type on white or navy.

## Typography

**Inter** is the primary family, used for everything: headings, body, UI labels, and numbers.
It is chosen for legibility at small sizes and across scripts.

Weights in use: **Light · Regular · Medium · SemiBold · Bold · ExtraBold**.

A practical assignment: Regular for body, Medium and SemiBold for labels and sub-headings,
Bold and ExtraBold for headings and emphasis, and Light sparingly for large quiet text.
Prefer one or two weights per screen.

**Plus Jakarta Sans** is the alternate display family. Use it **only** for hero and display
headings, and sparingly — a splash title, a single hero line. If a screen already has a
heading in Inter, that is usually the right answer; reach for Plus Jakarta Sans when a screen
should feel like a poster rather than a page.

## Spacing scale

Everything sits on a **base 8px grid**: 8 is the default step, and 4 is the half-step for
tight inline gaps.

`4 · 8 · 12 · 16 · 24 · 32 · 48` (pixels)

Use the scale rather than inventing values. Prefer the larger end between sections and the
smaller end within a component.

## Corner radius

`2 · 4 · 8 · 12 · 16 · 24` (pixels)

Small radii (2–4) for dense or inline elements such as chips and inputs; 8–12 for cards and
buttons; 16–24 for large surfaces and sheets. One radius per component, and consistent within
a screen.

## Elevation

Four levels, **1 to 4**, rendered as progressively stronger drop shadows. Higher means closer
to the reader.

- **Level 1** — resting cards and inputs.
- **Level 2** — raised cards, hover and focus states.
- **Level 3** — floating elements: a bar that sits above content, the compose row.
- **Level 4** — overlays: modals and sheets.

Do not stack elevation to manufacture emphasis. Two elements at the same level are at the
same depth.

## Iconography

- **Outline style** by default, not filled.
- **Consistent stroke weight** across a set — icons should look drawn by one hand.
- Drawn on a **24px, 32px, or 48px grid**, chosen by context: 24 for inline and list icons,
  32 for emphasis, 48 for empty states and feature marks.

Icons carry no colour of their own beyond the palette; they take the colour of the text they
sit beside unless they are the primary action.

## Taglines

| Tagline | Text | Use it when |
| --- | --- | --- |
| **Primary** | "Different languages. One human story." | The default. Launch screens, store listings, the top of a page — anywhere the brand introduces itself. |
| **Alternate** | "Tying human stories, languages, and truths together." | Longer-form and internal contexts: the repository, documentation, a mission statement. |

The primary tagline leads. Use the alternate when the primary's parallel structure would feel
too terse, or when the copy already sits inside Knot's own voice.

## Brand usage

### Do

- Keep **clear space** around the logo and logomark, and never crowd them.
- Use **approved colours** from the palette and the three gradients.
- **Maintain legibility**: check contrast before placing type on a gradient or an image.
- Use the **official assets** for the surface you are on (see the variations table).

### Don't

- Do **not stretch, squash, or rotate** the logo or logomark.
- Do **not change the colours** of an asset, or sample a colour from a screenshot.
- Do **not add effects** — no drop shadows, strokes, or glows on a mark that ships flat.
- Do **not alter the logo or logomark**: no rearranging, no replacing a letter, no recreating
  it in a different font, no redrawing the symbol.

## Asset source note

This document is a **specification, not the assets themselves**. The canonical source files —
SVG and AI/EPS for the logo, logomark, wordmark, the variations, and the app icons; OTF/TTF
for the fonts — are held by the founder and are deliberately **not committed to this
repository**. Binary design assets are large, opaque in review, and drift from their source;
keeping them out of git leaves this document as the single readable reference.

Recommended formats when assets are handed over:

| Asset | Format |
| --- | --- |
| Logos, logomark, wordmark, app-icon marks | **SVG** (the working vector format) |
| Raster previews and store listings | **PNG** |
| Editable masters | **AI / EPS** |
| Fonts (Inter, Plus Jakarta Sans) | **OTF / TTF** |

**Current gap:** none of this is implemented in the mobile app yet. The shipped screens
predate this specification and use their own placeholder colours and spacing. Turning the
tokens above into shared React Native style primitives is the job of **KNOT-001b — Design
System Implementation (mobile)**, queued in [`docs/ROADMAP.md`](ROADMAP.md). Until that
lands, treat this document as the target, not as a description of the current code.
