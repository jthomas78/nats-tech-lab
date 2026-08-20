#!/usr/bin/env python3
"""Generate and validate the operating-areas GeoJSON using only stdlib Python."""

from __future__ import annotations

import argparse
import json
import math
from collections import Counter
from pathlib import Path
from typing import Any


EXPECTED: dict[str, tuple[str, str]] = {
    "ZA-EC": ("Eastern Cape", "ZA"),
    "ZA-FS": ("Free State", "ZA"),
    "ZA-GP": ("Gauteng", "ZA"),
    "ZA-KZN": ("KwaZulu-Natal", "ZA"),
    "ZA-LP": ("Limpopo", "ZA"),
    "ZA-MP": ("Mpumalanga", "ZA"),
    "ZA-NW": ("North West", "ZA"),
    "ZA-NC": ("Northern Cape", "ZA"),
    "ZA-WC": ("Western Cape", "ZA"),
    "BW-CE": ("Central", "BW"),
    "BW-CH": ("Chobe", "BW"),
    "BW-GH": ("Ghanzi", "BW"),
    "BW-KG": ("Kgalagadi", "BW"),
    "BW-KL": ("Kgatleng", "BW"),
    "BW-KW": ("Kweneng", "BW"),
    "BW-NE": ("North-East", "BW"),
    "BW-NW": ("North-West", "BW"),
    "BW-SE": ("South-East", "BW"),
    "BW-SO": ("Southern", "BW"),
    "NA-CA": ("Zambezi", "NA"),
    "NA-ER": ("Erongo", "NA"),
    "NA-HA": ("Hardap", "NA"),
    "NA-KA": ("ǁKaras", "NA"),
    "NA-KH": ("Khomas", "NA"),
    "NA-KU": ("Kunene", "NA"),
    "NA-OD": ("Otjozondjupa", "NA"),
    "NA-OH": ("Omaheke", "NA"),
    "NA-OK": ("Kavango", "NA"),
    "NA-ON": ("Oshana", "NA"),
    "NA-OS": ("Omusati", "NA"),
    "NA-OT": ("Oshikoto", "NA"),
    "NA-OW": ("Ohangwena", "NA"),
}

ZA_BY_SOURCE_ISO = {
    "EC": "ZA-EC",
    "FS": "ZA-FS",
    "GT": "ZA-GP",
    "KZ": "ZA-KZN",
    "LI": "ZA-LP",
    "MP": "ZA-MP",
    "NW": "ZA-NW",
    "NC": "ZA-NC",
    "WC": "ZA-WC",
}

BOUNDS = (11.0, -35.0, 33.0, -16.5)  # -16.5, not -17: Namibia's Kunene
                                     # region genuinely reaches -16.9511 (the
                                     # Kunene River border with Angola). A -17
                                     # ceiling truncates it into a straight line.


def rounded_position(position: list[Any]) -> list[float]:
    if len(position) < 2:
        raise ValueError(f"position has fewer than two coordinates: {position!r}")
    return [round(float(position[0]), 4), round(float(position[1]), 4)]


def clipped_ring(ring: list[list[Any]]) -> list[list[float]]:
    """Clip a linear ring to the required lon/lat envelope."""
    points = [[float(position[0]), float(position[1])] for position in ring]
    if points and points[0] == points[-1]:
        points.pop()

    def clip_edge(axis: int, boundary: float, keep_greater: bool) -> None:
        nonlocal points
        if not points:
            return
        result: list[list[float]] = []
        previous = points[-1]
        previous_inside = (
            previous[axis] >= boundary if keep_greater else previous[axis] <= boundary
        )
        for current in points:
            current_inside = (
                current[axis] >= boundary if keep_greater else current[axis] <= boundary
            )
            if current_inside != previous_inside:
                delta = current[axis] - previous[axis]
                fraction = (boundary - previous[axis]) / delta
                intersection = [
                    previous[0] + fraction * (current[0] - previous[0]),
                    previous[1] + fraction * (current[1] - previous[1]),
                ]
                intersection[axis] = boundary
                result.append(intersection)
            if current_inside:
                result.append(current)
            previous = current
            previous_inside = current_inside
        points = result

    min_lon, min_lat, max_lon, max_lat = BOUNDS
    clip_edge(0, min_lon, True)
    clip_edge(0, max_lon, False)
    clip_edge(1, min_lat, True)
    clip_edge(1, max_lat, False)
    if points:
        points.append(points[0])
    return points


def rounded_ring(ring: list[list[Any]]) -> list[list[float]]:
    points: list[list[float]] = []
    for position in clipped_ring(ring):
        point = rounded_position(position)
        if not points or point != points[-1]:
            points.append(point)
    if points and points[0] != points[-1]:
        points.append(points[0])
    if len(points) < 4 or len({tuple(point) for point in points[:-1]}) < 3:
        raise ValueError("rounding produced a degenerate linear ring")
    return points


def rounded_geometry(geometry: dict[str, Any]) -> dict[str, Any]:
    geometry_type = geometry.get("type")
    coordinates = geometry.get("coordinates")
    if geometry_type == "Polygon":
        rounded = [rounded_ring(ring) for ring in coordinates]
    elif geometry_type == "MultiPolygon":
        rounded = [
            [rounded_ring(ring) for ring in polygon] for polygon in coordinates
        ]
    else:
        raise ValueError(f"unsupported geometry type: {geometry_type!r}")
    return {"type": geometry_type, "coordinates": rounded}


def load_features(path: Path) -> list[dict[str, Any]]:
    with path.open(encoding="utf-8") as source_file:
        document = json.load(source_file)
    if document.get("type") != "FeatureCollection":
        raise ValueError(f"{path} is not a FeatureCollection")
    return document["features"]


def generate(source_dir: Path, output_path: Path) -> None:
    curated: list[dict[str, Any]] = []
    source_files = (
        ("ZAF-ADM1.geojson", "ZA"),
        ("BWA-ADM1.geojson", "BW"),
        ("NAM-ADM1.geojson", "NA"),
    )

    for filename, country in source_files:
        for feature in load_features(source_dir / filename):
            source_properties = feature["properties"]
            source_iso = source_properties["shapeISO"]

            if source_iso == "AO-CNN":
                continue
            if country == "ZA":
                code = ZA_BY_SOURCE_ISO[source_iso]
            else:
                code = source_iso

            name, expected_country = EXPECTED[code]
            if expected_country != country:
                raise ValueError(f"unexpected country mapping for {code}")
            curated.append(
                {
                    "type": "Feature",
                    "id": code,
                    "properties": {
                        "code": code,
                        "name": name,
                        "country": country,
                        "level": "REGION",
                    },
                    "geometry": rounded_geometry(feature["geometry"]),
                }
            )

    curated.sort(key=lambda feature: feature["properties"]["code"])
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", encoding="utf-8", newline="\n") as output_file:
        json.dump(
            {"type": "FeatureCollection", "features": curated},
            output_file,
            ensure_ascii=False,
            separators=(",", ":"),
        )
        output_file.write("\n")


def positions(coordinates: Any):
    if (
        isinstance(coordinates, list)
        and len(coordinates) >= 2
        and all(isinstance(value, (int, float)) for value in coordinates[:2])
    ):
        yield coordinates
        return
    if isinstance(coordinates, list):
        for child in coordinates:
            yield from positions(child)


def validate(output_path: Path) -> None:
    raw = output_path.read_bytes()
    document = json.loads(raw.decode("utf-8"))
    assert document.get("type") == "FeatureCollection"
    assert set(document) == {"type", "features"}
    features = document["features"]
    assert len(features) == 32

    codes = [feature["properties"]["code"] for feature in features]
    assert len(codes) == len(set(codes))
    assert set(codes) == set(EXPECTED)
    assert Counter(feature["properties"]["country"] for feature in features) == {
        "ZA": 9,
        "BW": 10,
        "NA": 13,
    }

    by_code = {feature["properties"]["code"]: feature for feature in features}
    for code, (name, country) in EXPECTED.items():
        feature = by_code[code]
        assert set(feature) == {"type", "id", "properties", "geometry"}
        assert feature["type"] == "Feature"
        assert feature["id"] == code
        assert feature["properties"] == {
            "code": code,
            "name": name,
            "country": country,
            "level": "REGION",
        }

    assert all(feature["properties"]["country"] != "AO" for feature in features)
    assert all(feature["properties"]["name"] != "Cunene" for feature in features)
    assert by_code["NA-KA"]["properties"]["name"].encode("utf-8") == (
        b"\xc7\x81Karas"
    )
    assert by_code["NA-CA"]["properties"]["name"] == "Zambezi"
    assert by_code["ZA-NC"]["properties"]["name"] == "Northern Cape"

    position_count = 0
    for feature in features:
        geometry = feature["geometry"]
        assert geometry["type"] in {"Polygon", "MultiPolygon"}
        assert geometry["coordinates"]
        feature_positions = list(positions(geometry["coordinates"]))
        assert feature_positions
        position_count += len(feature_positions)
        for longitude, latitude, *_ in feature_positions:
            assert math.isfinite(longitude) and math.isfinite(latitude)
            assert 11 <= longitude <= 33
            assert -35 <= latitude <= -16.5
            assert longitude == round(longitude, 4)
            assert latitude == round(latitude, 4)

    print(f"PASS: parsed RFC 7946 FeatureCollection: {output_path}")
    print("PASS: 32 unique expected codes; counts ZA=9, BW=10, NA=13")
    print("PASS: AO-CNN/Cunene excluded; Namibia and South Africa mappings verified")
    print(f"PASS: {position_count} positions; non-empty Polygon/MultiPolygon geometries")
    print("PASS: all coordinates are [longitude, latitude], in bounds, rounded to 4 decimals")
    click_offset = raw.index(b"\xc7\x81Karas")
    print(f"PASS: UTF-8 ǁKaras prefix bytes are {raw[click_offset:click_offset + 2].hex(' ')}")
    print(f"PASS: file size is {len(raw):,} bytes")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("geojson", type=Path, help="output GeoJSON to validate")
    parser.add_argument(
        "--generate-from",
        metavar="SOURCE_DIR",
        type=Path,
        help="generate from ZAF/BWA/NAM ADM1 files before validating",
    )
    args = parser.parse_args()
    if args.generate_from:
        generate(args.generate_from, args.geojson)
    validate(args.geojson)


if __name__ == "__main__":
    main()
