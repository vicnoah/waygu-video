"""Configure the loopback-only Docker fixtures after compose up."""

import json
from http.client import HTTPException
import os
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


USERNAME = "waygu-test"
PASSWORD = os.environ.get("WAYGU_TEST_PASSWORD", "waygu-fixture-local")


def request(base, path, *, method="GET", params=None, body=None, headers=None):
    address = base + path
    if params:
        address += "?" + urlencode(params, doseq=True)
    request_headers = {"Accept": "application/json", **(headers or {})}
    data = None
    if body is not None:
        request_headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    with urlopen(Request(address, data, request_headers, method=method), timeout=10) as response:
        content = response.read()
    if content and content.lstrip().startswith((b"{", b"[")):
        return json.loads(content)
    return None


def wait_until_ready(operation, timeout=90):
    deadline = time.monotonic() + timeout
    while True:
        try:
            result = operation()
            if result is None:
                raise ValueError("readiness endpoint returned no JSON")
            return result
        except HTTPError as error:
            if error.code not in (502, 503, 504) or time.monotonic() >= deadline:
                raise
        except (OSError, HTTPException, ValueError):
            if time.monotonic() >= deadline:
                raise
        time.sleep(2)


def wait_for(base, path):
    return wait_until_ready(lambda: request(base, path))


def setup_media_browser(provider, base):
    def public_info():
        info = request(base, "/System/Info/Public")
        return info if info and info.get("Version") else None

    info = wait_until_ready(public_info)
    media_browser = 'MediaBrowser Client="Waygu Fixtures", Device="Docker", DeviceId="waygu-fixtures", Version="1.0"'
    headers = {"Authorization" if provider == "jellyfin" else "X-Emby-Authorization": media_browser}

    def login(password):
        return request(base, "/Users/AuthenticateByName", method="POST", headers=headers,
                       body={"Username": USERNAME, "Pw": password})

    try:
        user = wait_until_ready(lambda: login(PASSWORD))
    except HTTPError as error:
        if error.code not in (400, 401, 403, 404) or info.get("StartupWizardCompleted"):
            raise
        # Reading the wizard user creates its initial account on an empty data volume.
        wait_for(base, "/Startup/User")
        request(base, "/Startup/User", method="POST", body={"Name": USERNAME, "Password": PASSWORD})
        request(base, "/Startup/Complete", method="POST", body={})
        user = wait_until_ready(lambda: login(PASSWORD))

    if provider == "jellyfin":
        headers["Authorization"] += ', Token="' + user["AccessToken"] + '"'
        existing = request(base, "/Library/VirtualFolders", headers=headers)
    else:
        headers["X-Emby-Token"] = user["AccessToken"]
        existing = request(base, "/Library/VirtualFolders/Query", headers=headers)["Items"]

    names = {folder["Name"] for folder in existing}
    for name, kind in (("Movies", "movies"), ("Shows", "tvshows")):
        if name in names:
            continue
        path = "/media/" + name
        if provider == "jellyfin":
            request(base, "/Library/VirtualFolders", method="POST", headers=headers,
                    params={"name": name, "collectionType": kind, "paths": path, "refreshLibrary": "true"},
                    body={"LibraryOptions": {"PathInfos": [{"Path": path}]}})
        else:
            request(base, "/Library/VirtualFolders", method="POST", headers=headers,
                    body={"Name": name, "CollectionType": kind, "Paths": [path], "RefreshLibrary": True,
                          "LibraryOptions": {"PathInfos": [{"Path": path}]}})
    request(base, "/Library/Refresh", method="POST", headers=headers)
    print(f"{provider} {info['Version']}: configured fixture libraries")


def setup_plex(base):
    def identity_when_ready():
        identity = request(base, "/identity")
        if not identity:
            return None
        container = identity["MediaContainer"]
        if container.get("startState") in (None, "complete"):
            return container
        return None

    info = wait_until_ready(identity_when_ready, timeout=180)
    sections = wait_for(base, "/library/sections")["MediaContainer"]
    names = {folder["title"] for folder in sections.get("Directory", [])}
    for name, kind, scanner, agent in (
        ("Movies", "movie", "Plex Movie", "tv.plex.agents.movie"),
        ("Shows", "show", "Plex TV Series", "tv.plex.agents.series"),
    ):
        if name not in names:
            request(base, "/library/sections", method="POST", params={
                "name": name, "type": kind, "scanner": scanner, "agent": agent,
                "language": "en-US", "location": "/media/" + name,
            })
    sections = request(base, "/library/sections")["MediaContainer"]
    for section in sections.get("Directory", []):
        if section["title"] in {"Movies", "Shows"}:
            request(base, f"/library/sections/{section['key']}/refresh")
    print(f"plex {info['version']}: configured fixture libraries")


if __name__ == "__main__":
    setup_media_browser("jellyfin", "http://127.0.0.1:18096")
    setup_media_browser("emby", "http://127.0.0.1:28096/emby")
    setup_plex("http://127.0.0.1:32400")
