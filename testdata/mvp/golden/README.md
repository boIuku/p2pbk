# Sanitized MVP compatibility fixtures

These fixtures describe the current supported experimental on-disk formats
without credentials, private keys, source data, peer addresses, or real
repository contents. They are compatibility references for M0 and are not
production recovery material.

- `config.json` is a redacted default-shaped configuration.
- `storage.mode` is the persisted repository mode marker.
- `manifests.json` and `locators.json` are minimal versioned JSON shape samples.
- `recovery-bundle.json` is the decrypted metadata shape only; it is never an
  encrypted bundle and cannot be imported.

Experimental repositories without the current mode/ownership metadata remain
unsupported and must be rejected rather than silently migrated.
