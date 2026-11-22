#!/usr/bin/env python3
"""Keep the review inventory in sync with the Scenra screens and panels."""

import argparse
from pathlib import Path
import re
import shlex


ROOT = Path(__file__).resolve().parents[2]
DESIGN = ROOT / "design"
INVENTORY = ROOT / "docs" / "ui-review" / "screens.md"
HEADER = re.compile(r"^design(?: .*)?$", re.MULTILINE)
PANEL = re.compile(r'\bpanel=(?:"([^"]+)"|([^\s"]+))')
REVIEWS = {
    "welcome.scenra": "welcome.md",
    "add-source.scenra": "add-source.md",
    "source-login.scenra": "source-login.md",
    "home.scenra": "home.md",
    "library.scenra": "phone-library.md",
    "search.scenra": "phone-search.md",
    "movie-detail.scenra": "phone-movie.md",
    "series-detail.scenra": "phone-series.md",
    "tablet-home.scenra": "tablet-home.md",
    "tablet-detail.scenra": "tablet-detail.md",
    "tv-home.scenra": "tv-home.md",
    "tv-detail.scenra": "tv-detail.md",
    "tv-player.scenra": "tv-player.md",
    "tv-settings.scenra": "tv-settings.md",
    "desktop-main.scenra": "desktop-home.md",
    "desktop-player.scenra": "desktop-player.md",
    "desktop-settings.scenra": "desktop-settings.md",
    "views/desktop-settings-danmaku.scenra": "desktop-settings.md",
    "views/desktop-settings-general.scenra": "desktop-settings.md",
    "views/desktop-settings-playback.scenra": "desktop-settings.md",
    "views/desktop-settings-sources.scenra": "desktop-settings.md",
    "views/desktop-library.scenra": "desktop-library.md",
    "views/desktop-movie.scenra": "desktop-movie.md",
    "settings.scenra": "phone-source-settings.md",
    "settings-sources.scenra": "phone-source-settings.md",
    "player.scenra": "phone-player.md",
    "player-tracks.scenra": "phone-player.md",
    "player-danmaku.scenra": "phone-player.md",
    "settings-playback.scenra": "phone-player-settings.md",
    "settings-parental.scenra": "phone-parental.md",
    "settings-appearance.scenra": "phone-appearance.md",
    "settings-about.scenra": "phone-about.md",
    "settings-danmaku.scenra": "phone-danmaku-settings.md",
    "settings-subtitles.scenra": "phone-player-settings.md",
    "settings-storage.scenra": "storage-downloads.md",
}


def screens():
    for path in sorted([*DESIGN.glob("*.scenra"), *DESIGN.glob("views/*.scenra")]):
        source = path.read_text()
        headers = list(HEADER.finditer(source))
        if not headers:
            raise ValueError(f"{path.relative_to(ROOT)} contains no design")
        for index, header in enumerate(headers):
            tokens = shlex.split(header.group())[1:]
            attrs = dict(token.split("=", 1) for token in tokens if "=" in token)
            variant = next((token for token in tokens if "=" not in token), "default")
            end = headers[index + 1].start() if index + 1 < len(headers) else len(source)
            panels = sorted({quoted or bare for quoted, bare in PANEL.findall(source[header.end():end])})
            yield path, header, attrs, variant, panels, source


def document():
    variants = list(screens())
    files = len({entry[0] for entry in variants})
    lines = [
        "# Scenra screen inventory",
        "",
        f"{files} design files and {len(variants)} screen variants. "
        "Panel states listed here also need their own interaction and screenshot evidence.",
        "",
        "The [review log](README.md) records captures and remaining differences. "
        "An inventory entry does not certify a screen.",
        "",
        "| Design | Variant | Device or host | Panels | Review |",
        "| --- | --- | --- | --- | --- |",
    ]
    for path, header, attrs, variant, panels, source in variants:
        relative = path.relative_to(DESIGN).as_posix()
        line = source.count("\n", 0, header.start()) + 1
        name = attrs.get("name", relative).replace("|", r"\|")
        location = attrs.get("in", attrs.get("device", "phone"))
        if "width" in attrs or "height" in attrs:
            location += f" · {attrs.get('width', 'auto')} × {attrs.get('height', 'auto')}"
        review = f"[Compared]({REVIEWS[relative]})" if relative in REVIEWS else "Pending"
        lines.append(
            f"| [{name}](../../design/{relative}#L{line}) | {variant} | "
            f"`{location}` | {', '.join(panels) or '—'} | {review} |"
        )
    return "\n".join(lines) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    generated = document()
    if args.check:
        if not INVENTORY.exists() or INVENTORY.read_text() != generated:
            parser.exit(1, "Screen inventory is stale. Run build/ui-review/inventory.py.\n")
    else:
        INVENTORY.parent.mkdir(parents=True, exist_ok=True)
        INVENTORY.write_text(generated)


if __name__ == "__main__":
    main()
