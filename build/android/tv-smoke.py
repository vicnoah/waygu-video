"""Exercise the installed Android TV launcher APK through its visible UI."""

import os
import re
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


def skip_onboarding_with_remote():
    visible("add-source")
    for _ in range(4):
        if any(node.get("resource-id") == "onboarding-later" and node.get("focused") == "true"
               for node in nodes_on_screen()):
            adb("shell", "input", "keyevent", "KEYCODE_DPAD_CENTER")
            return
        adb("shell", "input", "keyevent", "KEYCODE_DPAD_DOWN")
    raise RuntimeError("D-pad focus did not reach the onboarding skip button")


def press(test_id):
    match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", visible(test_id).get("bounds", ""))
    if match is None:
        raise RuntimeError(f"{test_id!r} has no screen bounds")
    x1, y1, x2, y2 = map(int, match.groups())
    if x2 <= x1 or y2 <= y1:
        raise RuntimeError(f"{test_id!r} is outside the screen")
    adb("shell", "input", "tap", str((x1 + x2) // 2), str((y1 + y2) // 2))


if __name__ == "__main__":
    skip_onboarding_with_remote()
    visible("empty-add-source")
    press("tv-tab-settings")
    press("settings-add-source")
    visible("choose-flymoo")
    press("back")
    visible("settings-add-source")
    print("Android TV launcher, onboarding, settings and source selection passed.")
