# Changelog

All notable changes to worm are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added
- Live update progress on the version screen: current stage, percentage bar, and
  downloaded/total bytes for the archive download, checksum verification, and install.

### Fixed
- Pasting the wrong kind of code no longer fails with a bare `wrong signal kind`;
  the manual screen explains which code belongs in the field, and engine errors
  name the expected kind.

## [v0.1.5] - 2026-10-29

### Added
- PipeWire audio backend: `pw-record`/`pw-play` are used when available, with
  `arecord`/`aplay` as the ALSA fallback.

### Changed
- Manual exchange now separates copying from pasting: after an offer or answer
  is generated it is shown on its own screen with copy/save actions, and the
  paste field opens only on request.
- Missing audio is reported as a warning instead of an error; manual calls stay
  connected and run silent.
- `c`/`s` are no longer swallowed in the paste field, so hex codes can be typed.

### Fixed
- `Copy offer`/`Copy answer` copied the paste field instead of the generated code.
- Repeated `Create offer` could fail with
  `InvalidModificationError: have-local-offer->SetLocal(offer)->have-local-offer`;
  offer and answer generation are now serialized and idempotent.
- Audio discovery message no longer says `aplay not found` on PipeWire systems.

## [v0.1.3] - 2026-10-29

### Added
- Release notes on the version screen: `WHAT'S NEW` is shown only when a newer
  release is available.
- `updater.Release` and `LatestRelease` expose tag, name, notes, and URL from a
  single GitHub API request.

### Changed
- Release notes are stripped of markdown, wrapped to the card width, and capped
  to the available terminal height.

## [v0.1.2] - 2026-10-29

### Fixed
- `install.sh` failed with a raw 404 when a release was still publishing; the
  installer now retries the asset download for up to a minute.

## [v0.1.1] - 2026-10-29

### Added
- `make release TAG=vX.Y.Z`: validates the tag, clean tree, branch, upstream
  state, and runs checks before creating the annotated tag.

### Changed
- README documents the release flow and the wait for the release workflow.

## [v0.1.0] - 2026-10-29

### Added
- End-to-end P2P voice calls over WebRTC with two connection modes: a signaling
  server (`Create a call` / `Join a call`) and manual SDP/ICE code exchange.
- TUI with call controls, mute, quality stats, and clipboard copy via native
  tools or OSC 52, plus save-to-file fallback.
- In-app signaling server configuration (`6`) with URL validation; an empty
  value keeps manual exchange only.
- Version screen (`4`) with update check and in-place self-update, and uninstall
  (`5`) with confirmation.
- `install.sh` for Linux and macOS: platform detection, checksum verification,
  PATH setup, `--update`, `--version`, `--uninstall`, `--purge`, `--dir`, and
  `--no-path`.
- Release workflow producing `worm_<version>_<os>_<arch>.tar.gz` archives and
  `checksums.txt`.
- Manual exchange UI with compact code previews and byte counts.

### Changed
- Scarlet-on-black theme with a minimal, airy layout: single-line menu, grouped
  entries, short hints, and no explanatory copy.
- Missing releases are reported as `no releases yet` instead of a GitHub error.
- Server configuration errors point at `6` for the in-app form.

### Fixed
- Self-update replaces the binary atomically and requires a restart.
