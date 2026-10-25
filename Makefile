.PHONY: brand test samples site

# Reproducibly render all brand deliverables into brand/ (see docs/BRAND_SPEC.md).
brand:
	cd go && go run ./tools/mkbrand -out ../brand -fonts ../assets/fonts

# Formatting + vet + all unit tests (live smoke test skipped unless SAKUNA_LIVE=1).
test:
	cd go && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...

# Live run: fetch + cards + decide + site.
# Writes snapshot to out/snapshot.json, cards + texts to samples/, site to out/site/.
samples:
	cd go && go run ./cmd/sakuna all -cache ../cache -out ../out -samples-out ../samples -site-out ../out/site -brand ../brand

# Rebuild only the static site from an existing snapshot + cards
# into out/site (main holds code only; publishing ships out/site
# to the orphan `site` branch via deploy/remote/site-publish.sh).
site:
	cd go && go run ./cmd/sakuna site -snapshot ../out/snapshot.json -cards ../samples -out ../out/site -base /sakuna-bantay/ -brand ../brand
