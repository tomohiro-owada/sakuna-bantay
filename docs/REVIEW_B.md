# Review of Phase B cards (I looked at the rendered PNGs)

## Bugs (must fix, add regression tests)
1. PHIVOLCS time zone bug: the list row "16 October 2026 - 05:13 PM" is ALREADY PHT, but the
   quake card shows "16 Oct 2026, 9:13 PM PHT" (8h shift). The href id 2026_1016_0913 is UTC.
   Parse list times with time.ParseInLocation(Asia/Manila). Add a test: that row → 17:13 PHT
   = 09:13 UTC.
2. Quake map bbox does not include Las Piñas: the "Las Piñas" point is drawn at the top-left
   inside the legend although Las Piñas is off-map. The bbox MUST contain both the epicenter
   and Las Piñas (plus margin ~0.6°), min span 4°; then the dashed ring centered on Las Piñas
   with radius = distance passes through the epicenter. Clip all drawing to the map frame.
   Don't draw a point label that overlaps the legend (move legend to the corner farthest from
   both points, or drop the legend entry duplication: legend "Las Piñas" + point label is
   redundant — keep only the point label and the epicenter legend).
3. Land fill seams: on both maps there are straight horizontal/vertical seams in the land fill
   (e.g. a straight edge across northern Luzon around lat ~18.5 and a vertical one around
   lon ~120, and a rectangle above Mindanao). Probably clipping/tiling artifacts in mkgeo or
   a mismatch between NE land and province fills. Fix so land looks continuous: draw NE land
   ONLY outside the Philippines (or draw NE land first and provinces on top with identical
   fill), and verify the clipped polygons have no spurious edges. Add a test that the
   rendered map has no long straight runs of land/sea boundary pixels inside the frame
   (or at least a unit test on clipping concave polygons).
4. Location text: strip leading zeros "073 km N 59° E of Baganga" → "73 km N 59° E of Baganga".

## Layout polish
5. Daily rain chart: fixed 0–100% y scale with faint gridlines at 25/50/75/100 and small %
   axis labels; show % value above bars >= 20%; when max < 20% write a centered note inside
   the chart "Mababa ang tsansa ng ulan · Low chance of rain all day". Bars wider (one per hour,
   small gap).
6. Daily chips: text must stay inside the chip with >= 12px padding; "Unhealthy for Sensitive
   Groups" currently touches the border → make chips taller or use 2 lines with smaller
   font. Label the AQI chip "US AQI · model est." (AQI is a model estimate too, not only PM2.5).
7. SAMPLE watermark: keep the corner "SAMPLE" badge but the diagonal watermark must not sit
   behind the big M value/text; put it over the map/visual area only, low alpha, or a
   full-width diagonal band at 12% alpha behind everything. It must remain clearly visible.
8. Typhoon card: the cyclone name wraps with a huge line gap ("Typhoon Rosal" / "(CHAMPI)") —
   use normal line height (~1.2) for wrapped text everywhere (fix the shared wrap helper).
   When few provinces are shaded, list them below (already done) — fine.
9. Taal gauge: redesign as 6 discrete colored segments (0 teal … 5 dark red) with small gaps;
   the level number centered on each segment outside the arc; the needle points to the CENTER
   of the current level's segment; current segment emphasized (full opacity, others 45%).
   Move the bulletin title and the red reminder lower with spacing so they don't crowd the
   gauge base; use the empty bottom space.
10. Header right side: show the "as of" time for the data (daily: forecast run/fetch time PHT,
   quake: event time, signal: CAP sent time, Taal: bulletin date). Keep.

After fixing, render all four from fixtures again to /tmp/sakuna-cards/ (and a synthetic
signal sample with several provinces incl. NCR/Cavite/Laguna/Batangas/Rizal at levels 1–3,
plus a synthetic near quake e.g. M5.6 30 km SW of Nasugbu, Batangas) and check them.
