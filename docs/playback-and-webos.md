# Playback model and LG webOS limitations

## Why "Play Playlist" behaves this way

The LG webOS Plex client does not expose a public remote API that lets this app reliably queue or control a Plex playlist directly on the TV.

So plex-smash-deck does this instead:

1. It asks Plex for the selected movie file metadata.
2. It builds direct media URLs that Plex can serve.
3. It sends those URLs to the TV using LG webOS media APIs.

In other words, playback is started by launching media on webOS from URLs backed by your Plex server, rather than pushing a native "playlist object" into the LG Plex app.

## Is this "streaming"?

Yes, practically speaking. The TV plays media over HTTP from your Plex server URL(s). The app is not downloading full files locally first.

## Consequences

- Queue/playlist parity with Plex client UX is limited.
- "Play selected" and generated playlist actions are best-effort launch/control.
- Behavior can vary by TV firmware and network conditions.

## Why we still do it

This path is the most reliable cross-device approach available for LG webOS automation today, and keeps Plex credentials server-side.

## SSAP probe (foreground app / play state)

You can run experimental LG SSAP queries against your TV:

```bash
go run ./cmd/lg-ssap-probe
```

This uses an **extended manifest permission list** (similar to community LG remote apps). The dashboard’s normal pairing flow uses a **smaller** manifest; if every call returns `401 insufficient permissions`, re-pairing after we align manifests with the probe may be required for read APIs.

On a typical webOS set, useful responses can include:

- **`ssap://com.webos.applicationManager/getForegroundAppInfo`** — which app is in the foreground (e.g. `com.webos.app.mediadiscovery` when the native player is up).
- **`ssap://com.webos.media/getForegroundAppInfo`** — `playState` (e.g. `playing`) and a `mediaId` handle; **human-readable titles are not guaranteed** in this payload.
- **`ssap://audio/getStatus`** / **`getVolume`** — volume and output routing.

Some calls (`listApps`, `getServiceList`, etc.) may still return `401` until permissions or signing match what the firmware expects.

## See also

- [Connecting your TV (LG webOS, pairing, Network menu notes)](connecting-your-tv.md)
