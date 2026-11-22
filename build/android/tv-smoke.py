"""Exercise the installed Android TV launcher APK through its visible UI."""

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


def select_with_remote(test_id, direction="KEYCODE_DPAD_DOWN"):
    visible(test_id)
    focused = ""
    for _ in range(8):
        focused = next((node.get("resource-id") for node in nodes_on_screen()
                        if node.get("focused") == "true"), "")
        if focused == test_id:
            adb("shell", "input", "keyevent", "KEYCODE_DPAD_CENTER")
            return
        adb("shell", "input", "keyevent", direction)
    raise RuntimeError(f"D-pad focus did not reach {test_id!r}. Last focus was {focused!r}.")


if __name__ == "__main__":
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
