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

## Usage
See the [usage guide](docs/usage.md) for all options and details.

### 1. Configure what to back up
Create `~/.config/macup/config.yaml` and list the directories to back up:

```yaml
data:
  locations:
    - path: ~/Github
      ignore:          # file or directory names skipped at any depth
        - node_modules
        - .venv
    - path: ~/Documents
    - path: ~/.ssh
```

### 2. Create a backup
```sh
macup create -o ~/Backups/2026-10-20
```

Each location is stored as a compressed archive in the output directory,
together with a copy of the config used to create it.

The output can be any directory, including one on an external drive. Back up
straight onto the drive without needing space on your Mac first:

```sh
macup create -o /Volumes/MyDrive/macup/2026-10-20
```

To always back up to the same place, set `output: /Volumes/MyDrive/macup` in
your config and just run `macup create`.

### 3. Restore it
```sh
macup restore -b /Volumes/MyDrive/macup/2026-10-20
```

Extracts every location back to its original path, using the config stored in
the backup. Paths written with `~` are restored into the home directory of
whoever runs the restore, so backups move between Macs and user names.

### Back up .env files
```sh
macup env -o ./env-backup
```

Copies every `.env` and `.env.*` file from your locations as a plain,
uncompressed file, so you can grab a single one later without restoring a
whole backup. See the [usage guide](docs/usage.md#backing-up-env-files) for
more options.

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
