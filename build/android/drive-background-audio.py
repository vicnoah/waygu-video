import os
import shutil
import subprocess
import sys
import threading
import time

mode, serial, *test_args = sys.argv[1:]
adb = shutil.which("adb") or os.path.join(os.environ.get("ANDROID_HOME", ""), "platform-tools", "adb")
package = "dev.nsgo.wayguvideo.nsgotest"


def device(*args):
    return subprocess.check_output([adb, "-s", serial, *args], text=True)


def foreground_service():
    listing = device("shell", "dumpsys", "activity", "services", package)
    return "WayguPlaybackService" in listing and "isForeground=true" in listing


def wait_for_service(running):
    deadline = time.monotonic() + 3
    while foreground_service() != running:
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Android media playback service foreground={not running}, expected {running}")
        time.sleep(.1)


logcat = subprocess.Popen(
    [adb, "-s", serial, "logcat", "-v", "raw", "-T", "1", "-s", "nsgo:I"],
    stdout=subprocess.PIPE, text=True,
)
test = subprocess.Popen(test_args)
errors = []


def drive():
    try:
        for line in logcat.stdout:
            match line.strip():
                case "WAYGU_AUDIO_READY":
                    wait_for_service(mode == "on")
                    device("shell", "input", "keyevent", "HOME")
                    print("Sent Home after the clip started", flush=True)
                case "WAYGU_AUDIO_RUNNING":
                    wait_for_service(True)
                    sessions = device("shell", "dumpsys", "media_session")
                    if f"Media button session is {package}/WayguPlayback" not in sessions:
                        raise RuntimeError("The test player does not own Android's media buttons")
                    device("shell", "cmd", "media_session", "dispatch", "pause")
                    print("Playback advanced in the background. Sent the system pause command", flush=True)
                case "WAYGU_AUDIO_PAUSED":
                    wait_for_service(False)
                    device("shell", "am", "start", "-W", "-n", f"{package}/dev.oneo.nsgo.NsgoActivity")
                    print("The foreground service ended. Restored the activity", flush=True)
    except (RuntimeError, subprocess.CalledProcessError) as error:
        errors.append(str(error))
        test.terminate()


watcher = threading.Thread(target=drive, daemon=True)
watcher.start()
try:
    status = test.wait()
finally:
    if test.poll() is None:
        test.terminate()
    logcat.terminate()
    logcat.wait()
    watcher.join(timeout=3)
if errors:
    sys.exit(errors[0])
sys.exit(status)
