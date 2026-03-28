# Connecting your TV

This app talks to TVs in two different ways:

- **Plex “player” discovery** — uses Plex’s own APIs so you can pick a client name (works for many devices Plex knows about).
- **Direct playback on LG webOS** — uses LG’s **SSAP** WebSocket API on **port 3001** to launch the TV’s **native media player** with your Plex media URLs (see [Playback model and LG webOS limitations](playback-and-webos.md)).

**Samsung, Sony, and other brands:** direct-TV automation like LG SSAP is **not implemented yet**. If you want it, open an issue and describe your TV model and what you want to control — we can prioritize it.

---

## LG webOS (supported today)

### What you need on the network

- TV and the machine running **plex-smash-deck** on the **same LAN** (same Wi‑Fi or wired segment is fine).
- The TV’s **IP address** (Settings → Network → your Wi‑Fi or Ethernet connection → IP).
- For first-time control: the TV must **allow the connection** (pairing prompt on screen). After pairing, store `LGTV_ADDR` and `LGTV_CLIENT_KEY` in `.env` or Settings.

### Same prerequisites as Home Assistant’s LG webOS TV integration

Home Assistant’s official help for the **[LG webOS TV](https://www.home-assistant.io/integrations/webostv/)** integration is maintained as markdown in their docs repo ([`source/_integrations/webostv.markdown`](https://github.com/home-assistant/home-assistant.io/blob/current/source/_integrations/webostv.markdown)). That page describes the **same class of TV control** this app uses: LAN access to the TV’s WebSocket API (SSAP-style registration and pairing on **port 3001** with TLS on current firmware, same idea as `lgtv --ssl` in [LGWebOSRemote](https://github.com/klattimer/LGWebOSRemote)). Under the hood, Home Assistant Core uses the Python library **[aiowebostv](https://github.com/home-assistant-libs/aiowebostv)**.

From that document, the parts that apply here as well:

| Topic | What Home Assistant says (summary) |
|--------|-----------------------------------|
| **Before pairing** | Enable **LG Connect Apps** in the TV’s **Network** settings. On **older** models, the equivalent may be under **General** as **Mobile App** (wording varies). |
| **Pairing** | Turn the TV on, start pairing from the app, then **accept the pairing request on the TV** when it appears. |
| **Pairing fails** | Their troubleshooting step is: make sure **LG Connect Apps** is enabled in **Network**. |
| **Firewall / VLANs** | If the TV is not on the same L2 network as the server, you may need firewall rules allowing **TCP 3000 and 3001** from the server to the TV. |
| **Discovery** | Auto-discovery uses **SSDP**; that path also depends on the TV advertising itself on the LAN (related to the same network / “connect apps” story). |
| **Wake on LAN** | For power-on from the network, newer sets often need **Settings → General → Mobile TV On → Turn on via Wi‑Fi**; Home Assistant links to [these third‑party steps](https://support.quanticapps.com/hc/en-us/articles/115005985729-How-to-turn-on-my-LG-Smart-TV-using-the-App-WebOS-) as a guide. |

So: if you already followed Home Assistant’s LG TV setup, you have done the right TV-side prep for **plex-smash-deck** as well. If you **do not** see **LG Connect Apps** in Network, use the keypad / hidden-menu patterns in the next section (often recommended in Home Assistant community threads when that menu entry is missing).

### Why people mention a “secret” menu under Network

LG moves options between firmware versions. Some **webOS automation / home‑brew** guides say certain toggles (for example allowing **LG Connect Apps**, **Mobile TV On**, or similar “control from apps on this network” features) only show up after opening an **extra / hidden** view from **Settings → Network**.

**Those steps are not identical on every model.** Treat the patterns below as **things to try** if you do not see an obvious “allow remote / mobile / app” switch.

### Things to try (Network → your active connection)

Do this on the TV with the **Magic Remote** (or equivalent with number keys):

1. Open **Settings** → **Network** (on newer webOS: **All settings** → **General** → **Network** — exact path varies).
2. Open your **current** connection:
   - **Wi‑Fi:** open the **connected** network (the one checked / active).
   - **Ethernet:** open **Wired connection** / **Ethernet** details.
3. Look for any of:
   - **LG Connect Apps**
   - **Mobile TV On** / **Wake on LAN** (wording varies)
   - Anything about **external control**, **remote**, or **apps on network**
4. If you **do not** see those toggles, some community guides suggest opening a **hidden / extended** network page. **Not all TVs respond**; wrong sequences can open unrelated service menus, so go slowly:
   - With the **connection row** or **connection details** screen open, try entering **555** or **828** using the **number keys** (some TVs show a password / extra page).
   - Some models: with the connection highlighted, press **OK** **five times** quickly.
   - Some older guides: from Network, open the browser app and use special URLs — prefer the steps above first.

If your TV never shows extra options, it may already allow LAN control, or you may need **Developer Mode** (next section) for advanced tooling — that is separate from day‑to‑day playback.

### Developer Mode (optional, official LG path)

LG documents **Developer Mode** for webOS developers (install **Developer Mode** from the LG Content Store, sign in with an LG developer account, enable dev mode, **Key Server**, etc.). That path is mainly for installing and debugging apps, not strictly required for SSAP pairing — but some households already use it for other home‑brew tools.

Official entry point: [webOS TV Developer — Developer Mode App](https://webostv.developer.lge.com/develop/getting-started/developer-mode-app)

### Pairing with plex-smash-deck

1. Set `LGTV_ADDR` to the TV IP.
2. Leave `LGTV_CLIENT_KEY` empty on first connect (or clear it to re‑pair).
3. Start the server and trigger playback once — the TV should show a **permission / pairing** dialog.
4. Save the client key the app prints / persists.

If connection fails, confirm **nothing blocks port 3001** on the TV or router (same‑subnet client, no client isolation on guest Wi‑Fi, etc.). Also re-check **LG Connect Apps** / **Mobile App** as in the [Home Assistant LG webOS TV](https://www.home-assistant.io/integrations/webostv/) troubleshooting section.

---

## Other brands (Samsung, Sony, …)

**Coming soon upon request.** The architecture keeps LG SSAP in one place (`lgssap.go`), so additional targets can be added alongside it once we know the exact control protocol you need.
