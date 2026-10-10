# HMAC-SHA256, not plain SHA-256, for secret value hashing

SPEC.md §6 specifies plain SHA-256 for comparing secret values across applies without storing plaintext in `state.json`. A plain hash of a real-world secret (a short database password, say) is offline-crackable if `state.json` ever leaks, e.g. into a backup or support bundle. Decided (Phase 2 grilling, Q11) to deviate from SPEC and use HMAC-SHA256 keyed by a random per-project value at `.dex/secret.key` (gitignored, generated on first use) instead.

Losing the key is harmless: the next `dex plan` treats every secret as changed, `dex apply` re-sends the same `.env` values through the normal update path, and a fresh key is written. SPEC.md §6 to be reworded when this lands.
