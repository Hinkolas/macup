# macup
A Go-powered backup &amp; restore tool for macOS. Define folders, excludes,
apps, dev tools, and system tweaks in a central YAML config. Create clean
backups, auto-install essentials via Homebrew/App Store, restore dotfiles, and
reapply macOS settings for a consistent setup.

## Install
```sh
curl -fsSL https://raw.githubusercontent.com/Hinkolas/macup/main/install.sh | sh
```

The script installs the latest release for your Mac (Apple Silicon or Intel)
into `~/.local/bin` after verifying its checksum. Set `MACUP_INSTALL_DIR` to
install elsewhere, or `MACUP_VERSION` (e.g. `v0.1.0`) to pin a release.

## Upgrade
```sh
macup upgrade          # install the latest release
macup upgrade --check  # only check whether a newer release exists
```

## Back up .env files
```sh
macup env -o ./env-backup          # search the locations from your config
macup env -p ~/Github -o ./envs    # search specific directories instead
macup env --pattern '.env' --pattern '*.pem'  # choose which file names to copy
```

Copies every `.env` and `.env.*` file as a plain, uncompressed file, so you can
grab a single one later without restoring a whole backup. Copies keep their
path relative to your home directory (e.g. `envs/Github/app/.env`), and are
only readable by you since they usually contain secrets. Directories listed
under a location's `ignore` are skipped.

## Releasing
Releases are built by [GoReleaser](https://goreleaser.com) in GitHub Actions
when a version tag is pushed:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Tags with a pre-release suffix (e.g. `v0.2.0-rc.1`) are published as
pre-releases and are not picked up by `install.sh` or `macup upgrade`.

## Roadmap
- [] Create a backup according to the given configuration
- [] Restore all files, settings and programs from a created backup
- [] Implement a user-friendly TUI for configuring a backup
- [] Support for incremental backups
- [] Add synchronization with a remote file storage

## Feature Ideas
- [] Optimize backup performance by detecting compressabilty of certain file
     types and only compress files that are not already compressed (e.g. JPEG)
