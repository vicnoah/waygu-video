#!/usr/bin/env python3
"""Replay tablet home on Android and capture the composed display."""

import argparse
import base64
import os
import pathlib
import re
import subprocess
import threading


def run(serial, variant):
    root = pathlib.Path(__file__).resolve().parents[2]
    output = root / ".nsgo" / "tablet-home-android" / variant
    display = output / "display"
    display.mkdir(parents=True, exist_ok=True)
    adb = ["adb", "-s", serial]

    def shell(*args):
        return subprocess.check_output(adb + ["shell", *args], text=True, timeout=10).strip()

    dimensions = re.findall(r"(\d+)x(\d+)", shell("wm", "size"))
    width, height = map(int, dimensions[-1])
    density = int(re.findall(r"density: (\d+)", shell("wm", "density"))[-1]) / 160
    portrait = "1" if width > height else "0"
    landscape = "0" if width > height else "1"
    rotation = portrait if variant == "portrait" else landscape
    if variant == "portrait" and shell("settings", "get", "secure", "navigation_mode") != "2":
        raise SystemExit("Enable Android gesture navigation before running the portrait drawer test.")
    settings = {key: shell("settings", "get", "system", key) for key in ("accelerometer_rotation", "user_rotation")}
    failures = []
    captured = set()
    args = ["sh", "build/android/package.sh", "test", "-device", serial,
            "-env", "WAYGU_TEST_SERVERS=1"]
    env = os.environ.copy()
    if variant == "portrait":
        env["WAYGU_ANDROID_TEST_SYSTEM_INPUT"] = "1"
        args.extend(["-env", "WAYGU_TEST_ANDROID_TABLET_EDGE=1"])
    args.extend(["-run", f"^TestDockerTabletHomeReview$/^{variant}$", "-timeout", "4m",
                 "-screenshots", str(output.relative_to(root) / "window"), "."])

    def monitor():
        try:
            for line in logs.stdout:
                gesture = re.search(r"waygu-tablet-edge (\d+)$", line.rstrip())
                if gesture:
                    y = str(round(int(gesture[1]) * density))
                    shell("input", "swipe", "4", y, str(round(126 * density)), y, "350")
                elif line.rstrip().endswith("waygu-tablet-back"):
                    shell("input", "keyevent", "4")
                elif line.rstrip().endswith("waygu-tablet-rotate"):
                    shell("settings", "put", "system", "user_rotation", landscape)
                match = re.search(r"nsgo-screenshot ~([\w-]+) 1/\d+", line)
                if match:
                    token = match[1]
                    name = base64.urlsafe_b64decode(token + "=" * (-len(token) % 4)).decode()
                    if not re.fullmatch(r"tablet-home-(light|dark|portrait-drawer|portrait-closed)", name):
                        continue
                    image = subprocess.check_output(adb + ["exec-out", "screencap", "-p"], timeout=10)
                    if not image.startswith(b"\x89PNG\r\n\x1a\n"):
                        raise ValueError(f"Invalid Android display capture for {name}")
                    (display / (name + ".png")).write_bytes(image)
                    captured.add(name)
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            failures.append(error)

    try:
        shell("settings", "put", "system", "accelerometer_rotation", "0")
        shell("settings", "put", "system", "user_rotation", rotation)
        with subprocess.Popen(adb + ["logcat", "-v", "brief", "-T", "1", "-s", "nsgo:I"],
                              stdout=subprocess.PIPE, text=True) as logs:
            watcher = threading.Thread(target=monitor, daemon=True)
            watcher.start()
            try:
                subprocess.run(args, cwd=root, env=env, check=True)
            finally:
                logs.terminate()
                watcher.join(timeout=12)
    finally:
        for key, value in settings.items():
            if value == "null":
                shell("settings", "delete", "system", key)
            else:
                shell("settings", "put", "system", key, value)
    if failures:
        raise SystemExit(f"Display driver failed: {failures[0]}")
    expected = ("portrait-closed", "portrait-drawer") if variant == "portrait" else (variant,)
    for name in expected:
        if f"tablet-home-{name}" not in captured:
            raise SystemExit(f"Missing the tablet-home-{name} display capture")
    print(display)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("serial", help="Android tablet or emulator serial from adb devices")
    parser.add_argument("variant", choices=("light", "dark", "portrait"))
    options = parser.parse_args()
    run(options.serial, options.variant)
