# GeoIP updater workstream

## Implemented

- Added an optional updater that resolves one source commit and downloads only the fixed Country, ASN, and City MMDB names over HTTPS from allowlisted GitHub API/raw hosts.
- Added per-database byte ceilings, streamed SHA-256 calculations, MMDB format/type validation with the same MaxMind Go reader used by the parser, and atomic immutable candidate-generation staging.
- Added repeat validation before activation, explicit operator identity for activation, exact-path symlink activation under `/etc/geoip-db`, retention of the previously active generation, and a rollback operation.
- Refused unsafe updater-root symlinks, arbitrary existing GeoIP symlinks, directories, invalid MMDBs, and ordinary files at managed destinations. Existing manually provisioned files must be backed up and moved before the first managed activation.
- Added a separate private Supabase Storage bucket contract for the three content-addressed MMDB objects. Candidate publication requires explicit license acknowledgment and writes `geoip-candidates` metadata as `pending_approval` with automatic rollout disabled.
- Added source/license/attribution and operations documentation in `GEOIP_DATABASE_LIFECYCLE.md`.

## Verification

- `go test -count=1 ./internal/geoipdb ./internal/store`: PASS on the Windows development host using the repository's pinned Go toolchain.
- Tests cover invalid MMDB rejection, size-limit rejection, HTTPS/host redirect policy, license-consent gate, malformed candidate manifests, and the Supabase bucket's path/MIME/hash/size allowlist.

## Remaining integration

- The controller HTTP/UI must invoke candidate fetch and publication through authenticated administrator actions, record the approving user in audit history, and independently approve rollout before any client receives the files. This workstream intentionally does not add client auto-activation or download GeoIP data during log parsing.
- Linux RPM packaging and Rocky Linux symlink activation/rollback lifecycle should be run in the parent V15 release verification.
