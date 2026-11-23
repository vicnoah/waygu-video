"""Replay the download policy against Android's actual default network."""

import os
import shutil
import subprocess
import sys
import threading


serial = sys.argv[1] if len(sys.argv) > 1 else os.environ["ANDROID_SERIAL"]
adb = shutil.which("adb") or os.path.join(os.environ["ANDROID_HOME"], "platform-tools", "adb")


def device(*args):
    return subprocess.check_output([adb, "-s", serial, *args], text=True, timeout=15).strip()


def switch(kind):
    match kind:
        case "WIFI":
            device("shell", "svc", "data", "disable")
            device("shell", "svc", "wifi", "enable")
            device("shell", "cmd", "wifi", "connect-network", "AndroidWifi", "open")
        case "CELLULAR":
            device("shell", "svc", "data", "enable")
            device("shell", "svc", "wifi", "disable")
        case "OFFLINE":
            device("shell", "svc", "wifi", "disable")
            device("shell", "svc", "data", "disable")


previous_wifi = device("shell", "settings", "get", "global", "wifi_on")
previous_data = device("shell", "settings", "get", "global", "mobile_data")
logcat = None
test = None
watcher = None
errors = []

try:
    for port in (18096, 28096, 32400):
        device("reverse", f"tcp:{port}", f"tcp:{port}")
    switch("WIFI")
    logcat = subprocess.Popen(
        [adb, "-s", serial, "logcat", "-v", "raw", "-T", "1", "-s", "nsgo:I"],
        stdout=subprocess.PIPE, text=True,
    )
    test = subprocess.Popen([
        "sh", "build/android/package.sh", "test", "-device", serial,
        "-env", "WAYGU_TEST_SERVERS=1", "-env", "WAYGU_TEST_ANDROID_DOWNLOAD_NETWORK=1",
        "-run", "^TestDockerDownloadNetworkPolicy$", "-timeout", "150s",
        "-screenshots", ".nsgo/android-download-network",
    ])

    def drive():
        try:
            for line in logcat.stdout:
                for kind in ("WIFI", "CELLULAR", "OFFLINE"):
                    if line.strip() == f"WAYGU_DOWNLOAD_NETWORK_{kind}":
                        switch(kind)
                        print(f"Selected {kind.lower()} on the Android emulator", flush=True)
                        break
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
            errors.append(str(error))
            test.terminate()

    watcher = threading.Thread(target=drive, daemon=True)
    watcher.start()
    status = test.wait()
finally:
    if test is not None and test.poll() is None:
        test.terminate()
        test.wait()
    if logcat is not None:
        logcat.terminate()
        logcat.wait()
    if watcher is not None:
        watcher.join(timeout=3)
    if previous_wifi in ("0", "1"):
        device("shell", "svc", "wifi", "enable" if previous_wifi == "1" else "disable")
    if previous_data in ("0", "1"):
        device("shell", "svc", "data", "enable" if previous_data == "1" else "disable")

if errors:
    sys.exit(errors[0])
sys.exit(status)
