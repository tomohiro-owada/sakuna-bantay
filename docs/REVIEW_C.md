# Review of Phase C live run (samples/ from `sakuna all` live)

1. Signal card: synthetic Levels has "Metro Manila": 2 but the NCR polygons (geoBoundaries
   "NCR, City of Manila, First District", "NCR, Second/Third/Fourth District") are NOT shaded.
   Fix name matching so "Metro Manila"/"NCR"/"National Capital Region" (and CAP areaDesc
   variants) shade all 4 NCR district polygons. Regression test on the card's province→level
   lookup.
2. Synthetic cards must not look like real events: synthetic signal must use a clearly
   fictional cyclone name, e.g. "Bagyong SAMPLE (sample only)" — not the real "Rosal (CHAMPI)";
   synthetic quake "Source:" must read "Synthetic sample — not a real event" on the card and in
   the text (not "PHIVOLCS"). Source line of synthetic .txt: "SAMPLE ONLY — synthetic data".
3. Daily rain consistency: the card's chart note says "Low chance of rain all day" while the
   text says "May tsansa ng ulan (hanggang 31%)" and the header says "up to 31%". Use ONE
   number: the max hourly probability over the displayed window 06:00–23:00 (from hourly
   data), for header, chart note and text. Same thresholds in card and text
   (<20% low, 20–49% some chance, >=50% likely).
4. Post text translations: no untranslated English inside the Taglish and Japanese blocks
   except proper nouns / place strings from PHIVOLCS. Translate: alerts-strip line ("No active
   PAGASA warnings for NCR area" → Taglish "Walang aktibong babala ng PAGASA para sa NCR area",
   JP "NCR周辺にPAGASAの有効な警報はありません"), UV categories (JP 弱い/中程度/強い/非常に強い/極端に強い;
   Taglish can keep "High" with "mataas"), AQI categories (JP e.g. 敏感な人には健康に良くない),
   Taal level names (JP 0 平常, 1 低レベルの火山活動, 2 活動の高まり, 3 マグマ活動による不安,
   4 危険な噴火が切迫, 5 危険な噴火が発生中; Taglish keep EN term + short Filipino gloss).
5. Quake text: remove the debug line "latest notable: none; showing most relevant recent
   event" from post text; replace with a proper trilingual line, e.g. Taglish "Walang
   malakas na lindol malapit sa Las Piñas ngayon; ito ang pinakamalapit na kamakailang
   lindol (para sa impormasyon lang).", EN "No significant quake near Las Piñas; this is the
   most relevant recent event (for information only).", JP "ラスピニャス周辺で大きな地震は
   ありません。参考として最近の地震を表示しています。" Keep the manifest note as is.
6. Japanese compass: "NNEに約211 km" → use Japanese directions (北北東 etc.) in the JP block.
Update golden tests. Then rerun the live pipeline from cache ONLY (no new network beyond the
normal min-refetch cache: `cd go && go run ./cmd/sakuna all -cache ../cache -out ../out
-samples-out ../samples -site-out ../out/site -brand ../brand`), and run gofmt/vet/test.
