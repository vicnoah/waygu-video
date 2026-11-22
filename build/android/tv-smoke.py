"""Exercise the installed Android TV launcher APK through its visible UI."""

import argparse
import os
import subprocess
import time
import xml.etree.ElementTree as ET


SERIAL = os.environ["ANDROID_SERIAL"]
DUMP = "/data/local/tmp/waygu-tv-smoke.xml"


def adb(*args):
    result = subprocess.run(["adb", "-s", SERIAL, *args], capture_output=True, text=True, timeout=10)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return result.stdout


def nodes_on_screen():
    adb("shell", "uiautomator", "dump", DUMP)
    root = ET.fromstring(adb("exec-out", "cat", DUMP))
    return list(root.iter("node"))


def visible(test_id):
    deadline = time.monotonic() + 25
    diagnostic = "no accessibility nodes"
    while time.monotonic() < deadline:
        try:
            nodes = nodes_on_screen()
            for node in nodes:
                if node.get("resource-id") == test_id:
                    return node
            diagnostic = ", ".join(node.get("resource-id") for node in nodes if node.get("resource-id"))
        except (RuntimeError, subprocess.TimeoutExpired, ET.ParseError) as error:
            diagnostic = str(error)
        time.sleep(0.5)
    raise RuntimeError(f"{test_id!r} did not appear: {diagnostic}")


def visible_label(test_id):
    node = visible(test_id)
    return node.get("text") or node.get("content-desc")


def focused_id():
    deadline = time.monotonic() + 12
    while time.monotonic() < deadline:
        try:
            return next((node.get("resource-id", "") for node in nodes_on_screen()
                         if node.get("focused") == "true"), "")
        except (RuntimeError, subprocess.TimeoutExpired, ET.ParseError):
            time.sleep(0.3)
    raise RuntimeError("The active Android accessibility tree did not respond.")


def expect_focus(prefix):
    deadline = time.monotonic() + 15
    focused = ""
    while time.monotonic() < deadline:
        focused = focused_id()
        if focused.startswith(prefix):
            return focused
        time.sleep(0.3)
    raise RuntimeError(f"D-pad focus remained at {focused!r}, expected {prefix!r}.")


def key(*codes):
    adb("shell", "input", "keyevent", *(f"KEYCODE_{code}" for code in codes))


def focus_with_remote(test_id, direction):
    visible(test_id)
    focused = ""
    for _ in range(12):
        focused = focused_id()
        if focused == test_id:
            return
        adb("shell", "input", "keyevent", direction)
    raise RuntimeError(f"D-pad focus did not reach {test_id!r}. Last focus was {focused!r}.")


def select_with_remote(test_id, direction="KEYCODE_DPAD_DOWN"):
    focus_with_remote(test_id, direction)
    key("DPAD_CENTER")


def onboarding_smoke():
    select_with_remote("add-source")
    visible("scan-again")
    select_with_remote("choose-emby")
    visible("server-port")
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    visible("choose-emby")
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    select_with_remote("onboarding-later")
    visible("empty-add-source")
    select_with_remote("tv-tab-settings", "KEYCODE_DPAD_RIGHT")
    key("DPAD_RIGHT")
    select_with_remote("settings-add-source")
    visible("choose-flymoo")
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    visible("settings-add-source")
    print("Android TV onboarding, server setup and settings passed using only the remote.")


def open_featured_details():
    visible("home-hero-play")
    expect_focus("home-hero-play")
    key("DPAD_RIGHT", "DPAD_CENTER")
    expect_focus("detail-play")


def library_smoke():
    open_featured_details()
    key("BACK")
    expect_focus("home-hero-play")
    key("DPAD_DOWN")
    first_resume = expect_focus("resume-")
    key("DPAD_RIGHT")
    if expect_focus("resume-") == first_resume:
        raise RuntimeError("Right did not focus the next bookmark.")
    key("DPAD_DOWN")
    expect_focus("title-")
    print("Android TV home opened details and moved between both media shelves using the remote.")


def detail_smoke():
    open_featured_details()
    select_with_remote("playback-version", "KEYCODE_DPAD_RIGHT")
    expect_focus("tv-version-auto")
    key("BACK")
    expect_focus("playback-version")
    focus_with_remote("detail-play", "KEYCODE_DPAD_LEFT")
    visible("tv-detail-cast")
    key("DPAD_DOWN")
    expect_focus("tv-cast-")
    key("DPAD_DOWN")
    expect_focus("tv-detail-related-title-")
    key("DPAD_CENTER")
    expect_focus("detail-play")
    print("Android TV detail passed version, credits and recommendation navigation.")


def player_smoke():
    def position():
        seconds = 0
        for part in visible_label("player-position").split(":"):
            seconds = seconds * 60 + int(part)
        return seconds

    open_featured_details()
    key("DPAD_CENTER")
    visible("tv-player")
    expect_focus("toggle-play")
    playing = visible_label("toggle-play")
    key("DPAD_CENTER")
    if visible_label("toggle-play") == playing:
        raise RuntimeError("Select did not toggle playback.")

    key("MENU")
    visible("tv-player-quick-settings")
    expect_focus("toggle-danmaku")
    key("DPAD_DOWN")
    expect_focus("tv-player-opacity")
    opacity = visible_label("tv-player-opacity-value")
    key("DPAD_RIGHT" if opacity == "10%" else "DPAD_LEFT")
    if visible_label("tv-player-opacity-value") == opacity:
        raise RuntimeError("Left or Right did not adjust danmaku opacity.")
    key("BACK")
    expect_focus("toggle-play")

    previous = position()
    key("DPAD_LEFT" if previous >= 10 else "DPAD_RIGHT")
    if position() == previous:
        raise RuntimeError("Left or Right did not change playback position.")
    key("DPAD_UP")
    visible("player-tracks-dialog")
    key("BACK")
    visible("tv-player")
    key("BACK")
    expect_focus("detail-play")
    print("Android TV player passed Select, Menu, opacity, seeking, tracks and Back.")


def settings_smoke():
    visible("home-hero-play")
    key("DPAD_UP")
    select_with_remote("tv-tab-settings", "KEYCODE_DPAD_RIGHT")
    expect_focus("tv-settings-section-sources")
    visible("tv-pair-code")
    for section in ("playback", "subtitles", "danmaku"):
        key("DPAD_DOWN")
        expect_focus("tv-settings-section-" + section)
    key("DPAD_RIGHT")
    expect_focus("tv-danmaku-visible")
    for option in ("sources", "opacity"):
        key("DPAD_DOWN")
        expect_focus("tv-danmaku-" + option)
    before = visible_label("tv-danmaku-opacity-value")
    key("DPAD_RIGHT" if before == "10%" else "DPAD_LEFT")
    after = visible_label("tv-danmaku-opacity-value")
    if not before or not after or before == after:
        raise RuntimeError("The remote did not change the settings opacity.")
    for option in ("font-size", "speed", "area"):
        key("DPAD_DOWN")
        expect_focus("tv-danmaku-" + option)
    key("BACK")
    visible("tv-tab-settings")
    print("Android TV settings passed categories, options, opacity, pairing code and Back.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--with-library", action="store_true",
                      help="test the home screen with a connected media library and two resume bookmarks")
    mode.add_argument("--with-detail", action="store_true",
                      help="test a featured movie with versions, credits and recommendations")
    mode.add_argument("--with-player", action="store_true",
                      help="test remote playback against a featured movie")
    mode.add_argument("--with-settings", action="store_true",
                      help="test the TV settings categories and controls against a connected library")
    args = parser.parse_args()
    if args.with_player:
        player_smoke()
    elif args.with_settings:
        settings_smoke()
    elif args.with_detail:
        detail_smoke()
    elif args.with_library:
        library_smoke()
    else:
        onboarding_smoke()
