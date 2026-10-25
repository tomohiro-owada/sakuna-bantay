# Sakuna Bantay — brand spec (Part 1)

Sakuna Bantay is an UNOFFICIAL, non-profit weather & disaster watch for Las Piñas /
South Metro Manila (sibling of Presyo Bantay, which uses a cream background, flat
illustration, dark green + amber, Montserrat ExtraBold wordmark). Sakuna Bantay must feel
calm and trustworthy, NOT like a government agency: no seals, no laurels, no stars-and-rays,
no shields with ribbons, no PAGASA/PHIVOLCS/NDRRMC look-alikes, no flag colors combos.

## Palette (use these exact constants, defined once in Go)
- Navy      #0E2A3B (text, deep background)
- Teal      #0F6B6B (primary)
- Teal-lite #2A9D8F (secondary accents)
- Amber     #F2A516 (alert accent — use sparingly)
- Cream     #F7F2E6 (light background, like Presyo Bantay's cream)
- White     #FFFFFF

## Mark (logo symbol)
Concept: "watching the sky and the water". Inside a circle:
- deep teal (#0F6B6B → slightly darker toward navy at the bottom is OK, or flat) filled disc,
- a big, simple white rounded cloud in the upper-middle (3 overlapping round lobes + flat base),
- two smooth white/teal-lite wave lines in the lower part (water / flood),
- an amber "watch" element: two concentric radar arcs (like a signal/watch sweep, ~90° arcs)
  at the upper-right of the cloud, OR a small amber sun peeking behind the cloud's right
  shoulder. Choose radar arcs (bantay = watch). Thick strokes so it survives 40px.
- Max 3 visual ideas; thick shapes; must read well cropped as a small CIRCLE (FB profile).
The mark geometry must be defined ONCE as SVG markup (paths/circles in a 512x512 viewBox),
so the same geometry is used for the SVG logo, favicons and PNG rendering.

## Deliverables (all written to brand/ at repo root, reproducible by `make brand`)
Rendered by a Go tool `go/tools/mkbrand` (pure Go; rasterize SVG with
github.com/srwiley/oksvg + github.com/srwiley/rasterx; text via golang.org/x/image/font/opentype
with fonts from assets/fonts/ — Montserrat-ExtraBold/Bold/SemiBold, NotoSans-*, NotoSansJP-*.otf):
1. brand/sakuna-bantay-mark.svg — mark only (512 viewBox), no text.
2. brand/sakuna-bantay-logo.svg — horizontal lockup: mark + wordmark "Sakuna Bantay" (navy,
   Montserrat ExtraBold via font-family stack 'Montserrat, Noto Sans, sans-serif') + small
   tagline "Weather & disaster watch · Las Piñas". (SVG text is fine here.)
3. brand/profile-720.png — 720x720 FB profile picture: cream full-bleed background, the mark
   large and centered (~78% of width) so it reads inside FB's circular crop; NO text (text is
   unreadable at small size). Also brand/profile-180.png preview downscale.
4. brand/cover-1640x924.png — FB cover. Background: cream (#F7F2E6) with very subtle teal
   contour/wave lines (low-alpha) and maybe a soft navy band at the bottom edge only outside
   the safe area. All text and the mark MUST be inside the central safe area 1640x624
   (y from 150 to 774) and horizontally within x 220..1420 (mobile crops the sides; the
   desktop profile picture overlaps bottom-left). Layout: mark (~230px) on the left of a
   centered group, then to its right:
     - "Sakuna Bantay" Montserrat ExtraBold ~110px navy,
     - tagline "Weather & disaster watch · Las Piñas & South Metro" Montserrat SemiBold ~40px teal,
     - a short amber rule (like Presyo Bantay's red rule),
     - "Unofficial · Non-profit · Open data" Noto Sans SemiBold ~28px navy at ~70% alpha.
   The whole group horizontally centered on 820 and vertically centered on 462.
5. Site favicons into brand/favicon/: favicon.svg (mark), favicon-32.png, favicon-16.png,
   apple-touch-icon-180.png (mark on cream, small padding), icon-192.png, icon-512.png,
   logo-mark-96.png and logo-mark-144.png (transparent background, like Presyo Bantay's
   site/public/brand/logo-mark-*.png).
6. Small-size check: brand/check-circle-48.png = profile picture downscaled to 48px and masked
   to a circle (to eyeball legibility).

Anti-aliased, high quality downscaling (render big then golang.org/x/image/draw CatmullRom).
Unit test: the tool's render functions produce images of exactly the right sizes and the
cover's non-background pixels are confined within the safe area (check text bounding boxes).
