# Operating areas GeoJSON

`operating-areas.geojson` is a static Vite runtime asset containing 32 ADM1
operating areas for South Africa, Botswana, and Namibia. Coordinates are WGS84
longitude/latitude (RFC 7946), rounded to four decimal places. The file is not
imported through the frontend bundler.

## Sources and licences

Source: geoBoundaries `gbOpen` ADM1, release `9469f09`, simplified geometry:

- [South Africa ADM1 GeoJSON](https://raw.githubusercontent.com/wmgeolab/geoBoundaries/9469f09/releaseData/gbOpen/ZAF/ADM1/geoBoundaries-ZAF-ADM1_simplified.geojson) — CC BY 3.0 IGO
- [Botswana ADM1 GeoJSON](https://raw.githubusercontent.com/wmgeolab/geoBoundaries/9469f09/releaseData/gbOpen/BWA/ADM1/geoBoundaries-BWA-ADM1_simplified.geojson) — CC BY-SA 2.0
- [Namibia ADM1 GeoJSON](https://raw.githubusercontent.com/wmgeolab/geoBoundaries/9469f09/releaseData/gbOpen/NAM/ADM1/geoBoundaries-NAM-ADM1_simplified.geojson) — Public Domain
- [geoBoundaries project](https://www.geoboundaries.org/)

The source files used for regeneration must already exist locally. Regeneration
does not download data and requires only Python 3's standard library.

## Regenerate and validate

From the repository root, set the local source directory and run:

```sh
SOURCE_DIR=/private/tmp/claude-501/-Users-jeremy-dev-github-jthomas78-nats-tech-lab/a80c694c-a515-4418-a79e-854f68d7e00b/scratchpad/geo
OUTPUT=demos/01-dictionary/frontend/refdata/public/geo/operating-areas.geojson
SCRIPT=demos/01-dictionary/frontend/refdata/public/geo/validate_operating_areas.py
python3 "$SCRIPT" --generate-from "$SOURCE_DIR" "$OUTPUT"
```

Validate an existing generated file without rewriting it:

```sh
python3 demos/01-dictionary/frontend/refdata/public/geo/validate_operating_areas.py \
  demos/01-dictionary/frontend/refdata/public/geo/operating-areas.geojson
```

Generation applies the fixed code/name mappings in the script, rounds every
coordinate to four decimal places, removes identical consecutive rounded
vertices, and emits compact UTF-8 JSON. Geometry is **not** clipped to the
validator's coordinate envelope, and no additional simplification is applied.

## Important curation decisions

### Namibia

- **Angola contaminant removed:** `AO-CNN` / `Cunene` is dropped. Namibia's
  separate `NA-KU` / `Kunene` is retained.
- **Current names emitted:** `NA-CA` / `Caprivi` becomes `Zambezi`; `NA-KA` /
  `Karas` becomes `ǁKaras` (U+01C1 LATIN LETTER LATERAL CLICK).
- **Historical boundary retained:** `NA-OK` / `Kavango` remains one undivided
  region under the retired ISO code. The source geometry predates the 2013
  split into Kavango East (`NA-KE`) and Kavango West (`NA-KW`), and no split
  boundary is available; no replacement geometries are fabricated.
- **No envelope clipping (corrected 2026-08-20):** an earlier pass clipped
  `NA-KU` (Kunene) at latitude `-17` to satisfy a validator bound, flattening 21
  source vertices of the Kunene River border with Angola into a straight
  artificial line. The bound was wrong, not the geometry: Kunene genuinely
  reaches `-16.9511`, and Namibia's northernmost point is near `-16.95`. The
  envelope is now `-16.5` and exists purely as a sanity assertion — **the
  validator must never reshape geometry to satisfy itself.** `NA-KU` has been
  regenerated from source at full extent.

### South Africa

- Source codes are remapped as follows: `EC→ZA-EC`, `FS→ZA-FS`, `GT→ZA-GP`,
  `KZ→ZA-KZN`, `LI→ZA-LP`, `MP→ZA-MP`, `NW→ZA-NW`, `NC→ZA-NC`, and
  `WC→ZA-WC`.
- The source typo `Nothern Cape` is corrected to `Northern Cape`.
