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


def wait_for(base, path):
    deadline = time.monotonic() + 90
    while True:
        try:
            data = request(base, path)
            if data is None:
                raise ValueError("readiness endpoint returned no JSON")
            return data
        except (OSError, HTTPException, ValueError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(2)


def setup_media_browser(provider, base):
    info = wait_for(base, "/System/Info/Public")
    media_browser = 'MediaBrowser Client="Waygu Fixtures", Device="Docker", DeviceId="waygu-fixtures", Version="1.0"'
    headers = {"Authorization" if provider == "jellyfin" else "X-Emby-Authorization": media_browser}

    def login(password):
        return request(base, "/Users/AuthenticateByName", method="POST", headers=headers,
                       body={"Username": USERNAME, "Pw": password})

    try:
        user = login(PASSWORD)
    except HTTPError as error:
        if error.code not in (400, 401, 403, 404) or info.get("StartupWizardCompleted"):
            raise
        request(base, "/Startup/User", method="POST", body={"Name": USERNAME, "Password": PASSWORD})
        request(base, "/Startup/Complete", method="POST", body={})
        user = login(PASSWORD)

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
    info = wait_for(base, "/identity")["MediaContainer"]
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
