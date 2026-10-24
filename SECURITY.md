# Security and deployment

Use a release only on a network you trust, behind an authenticated access boundary. Older published versions do not authenticate dashboard management routes; they must not be exposed directly to the internet or untrusted clients. Management authentication and credential redaction are being added before the next supported release.

Runtime media, captions, thumbnails and playback history were removed from repository history in October 2026. Existing clones must be replaced with a fresh clone or carefully rebased onto the rewritten history. Do not merge or push old branches/tags back into this repository: that can restore removed data. Preserve your local application settings and media outside Git before replacing a clone.

Never commit .env files, pairing keys, tokens, passwords, runtime data or unrelated network operations scripts. Use synthetic test fixtures outside the runtime data directory. Do not post credentials or private logs to public issues or support rooms.

Repository cleanup cannot revoke downloaded copies. If an actual credential is exposed, revoke/rotate it with its issuing provider; rewriting Git alone is insufficient.
