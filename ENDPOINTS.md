# Endpoints: plex-dashboard

**Base URL:** `http://localhost:8081`

All responses: `{ "success": bool, "data": any, "error": "..." }`

**Note — LG TV playback:** Use the two-step flow: `POST /api/playlists/random` to create a playlist, then `POST /api/playlists/play` to send it to the LG TV via WebOS SSAP. The combined `random-play` endpoint uses a broken HTTP path for the LG TV.

---

## Health

### `GET /api/health`
Returns service health.

**Response:**
```json
{ "success": true }
```

---

## Players

### `GET /api/players`
List available Plex players and the configured target.

**Response:**
```json
{ "success": true, "data": { "players": [{ "Name": "Living Room", "ClientIdentifier": "tvdevice-lgtv-legacy", "Product": "Plex for LG" }], "targetClient": "Living Room" } }
```

---

## Playlists

### `POST /api/playlists/random`
Create a random playlist from the library (does not play it). Use with `POST /api/playlists/play` for LG TV.

**Body:**
```json
{ "title": "Movie Night", "count": 5 }
```

**Response:**
```json
{ "success": true, "data": { "Title": "Movie Night", "Count": 5, "FirstRatingKey": "1234" } }
```

---

### `POST /api/playlists/play`
Play an existing playlist on a player. For LG TV, uses WebOS SSAP (correct path). `clientName` defaults to configured target if omitted.

**Body:**
```json
{ "title": "Movie Night", "clientName": "Living Room" }
```

**Response:**
```json
{ "success": true, "data": { "PlaylistTitle": "Movie Night", "PlaylistCount": 5, "TargetClient": "Living Room", "sentTitles": ["The Godfather (1972)"] } }
```

---

### `POST /api/playlists/by-genre-rating`
Create a playlist filtered by genre and minimum rating (does not play it).

**Body:**
```json
{ "genre": "Action", "minRating": 8.0 }
```

**Response:**
```json
{ "success": true, "data": { "playlist": {}, "rule": { "genre": "Action", "minRating": 8.0 } } }
```

---

### `POST /api/playlists/by-people`
Create a playlist by actor or director.

**Body:**
```json
{ "title": "Kubrick Picks", "count": 10, "actor": "", "director": "Stanley Kubrick" }
```

**Response:**
```json
{ "success": true, "data": { "PlaylistTitle": "Kubrick Picks", "PlaylistCount": 10 } }
```

---

## Movies

### `GET /api/movies`
List all movies in the library (cached). Supports sort/filter via query params.

**Response:**
```json
{ "success": true, "data": [{ "ratingKey": "1234", "title": "The Godfather", "year": 1972, "rating": 9.2 }] }
```

---

### `POST /api/movies/play`
Play specific movies on a player. `transport`: `"webos"` for LG TV, `"companion"` for other Plex players.

**Body:**
```json
{ "items": [{ "ratingKey": "1234", "partKey": "/library/parts/5678", "container": "mp4", "title": "The Godfather" }], "clientName": "Living Room", "shuffle": false, "transport": "webos" }
```

**Response:**
```json
{ "success": true, "data": { "sent": 1, "client": "Living Room" } }
```

---

## Playback Control

### `POST /api/plex/companion/control`
Send a playback command to a Plex player (pause, play, skip, seek). Best for non-LG players.

**Body:**
```json
{ "clientName": "Living Room", "action": "pause", "seekMs": 0 }
```

Actions: `play`, `pause`, `skipNext`, `skipPrevious`, `seekto`

**Response:**
```json
{ "success": true, "data": { "target": "Living Room", "action": "pause" } }
```

---

## TV Volume

### `GET /api/lg/volume`
Get LG TV volume and mute state.

**Response:**
```json
{ "success": true, "data": { "supported": true, "volume": 20, "mute": false } }
```

---

### `POST /api/lg/volume`
Set LG TV volume (0–100).

**Body:**
```json
{ "level": 20 }
```

**Response:**
```json
{ "success": true, "data": { "supported": true, "volume": 20, "mute": false } }
```
