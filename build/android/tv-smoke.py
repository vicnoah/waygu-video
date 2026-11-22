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


def select_with_remote(test_id, direction="KEYCODE_DPAD_DOWN"):
    visible(test_id)
    focused = ""
    for _ in range(8):
        focused = focused_id()
        if focused == test_id:
            adb("shell", "input", "keyevent", "KEYCODE_DPAD_CENTER")
            return
        adb("shell", "input", "keyevent", direction)
    raise RuntimeError(f"D-pad focus did not reach {test_id!r}. Last focus was {focused!r}.")


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
    select_with_remote("settings-add-source")
    visible("choose-flymoo")
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    visible("settings-add-source")
    print("Android TV onboarding, server setup and settings passed using only the remote.")

def library_smoke():
    visible("home-hero-play")
    expect_focus("home-hero-play")
    key("DPAD_RIGHT")
    expect_focus("home-hero-details")
    key("DPAD_CENTER")
    visible("detail-play")
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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--with-library", action="store_true",
                        help="test the home screen with a connected media library and two resume bookmarks")
    if parser.parse_args().with_library:
        library_smoke()
    else:
        onboarding_smoke()
