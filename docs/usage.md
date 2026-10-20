# Using macup
This guide walks through the full backup and restore workflow: writing a
config, creating a backup (for example straight onto an external drive), and
restoring it on the same or a new Mac.

- [The config file](#the-config-file)
- [Creating a backup](#creating-a-backup)
- [Backing up to an external drive](#backing-up-to-an-external-drive)
- [What a backup contains](#what-a-backup-contains)
- [Restoring a backup](#restoring-a-backup)
- [Backing up .env files](#backing-up-env-files)
- [Testing a restore with clear](#testing-a-restore-with-clear)

## The config file
macup reads its config from `~/.config/macup/config.yaml` by default. Every
command that needs a config accepts `-c`/`--config` to use a different file.

```yaml
# Where backups are written when no -o/--output flag is given (default ./backup)
output: /Volumes/Backup/macup

data:
  locations:
    - path: ~/Github
      ignore:
        - node_modules
        - .venv
        - target
    - path: ~/Documents
    - path: ~/.ssh
    - path: ~/.config
      ignore:
        - .DS_Store
```

| Key | Description |
| --- | --- |
| `output` | Directory the backup is written to. `~` is expanded to your home directory, and relative paths are resolved from the directory you run macup in. |
| `data.locations` | List of directories to back up. At least one is required. |
| `data.locations[].path` | Directory to back up. `~` is expanded to your home directory. |
| `data.locations[].ignore` | File or directory names to skip, at any depth below the location. An ignored directory is skipped with everything inside it. |

Ignore entries are matched against the exact name of each file or directory,
not its path, and wildcards are not supported. `node_modules` skips every
`node_modules` directory below the location, and `foo/bar` matches nothing.

Write locations with `~` (e.g. `~/Github` rather than `/Users/you/Github`) if
you want to restore them on a Mac with a different user name, since `~` is
resolved to the home directory of whoever runs the restore.

## Creating a backup
```sh
macup create                         # use the config's output (or ./backup)
macup create -o ~/Backups/2026-10-20 # write the backup somewhere else
macup create -c ./work.yaml          # use a different config file
```

`create` writes one compressed archive per location into the output
directory, which is created if it doesn't exist. A progress bar per location
shows how far along each one is.

Files that can't be read, as well as sockets, named pipes and devices, are
skipped. Files that change while they are being archived are kept as far as
they could be read. All of these are listed once the backup is done.

Running `create` again with the same output directory replaces the archives in
it. Use a new directory per backup (e.g. one named after the date) to keep
older ones.

You can stop a backup at any time with `Ctrl+C`. Archives are only moved into
place once they are complete, so a cancelled backup never leaves a
half-written archive behind.

## Backing up to an external drive
Since the output can be any directory, you can back up directly to an external
drive without first creating the backup on your Mac and copying it over. This
also means the backup doesn't need free space on your internal disk.

Connected drives are mounted under `/Volumes`:

```sh
ls /Volumes
macup create -o /Volumes/MyDrive/macup/2026-10-20
```

To always back up to the drive, set it as `output` in your config:

```yaml
output: /Volumes/MyDrive/macup
```

A few things to keep in mind:

- Connect the drive before running `create`. Otherwise it stops with
  `failed to create output directory: … permission denied`, since
  `/Volumes/MyDrive` doesn't exist and only macOS may create it.
- The drive needs enough free space for the compressed archives.
- Use a drive formatted as APFS, Mac OS Extended or exFAT. FAT32 (shown as
  MS-DOS in Disk Utility) can't store files larger than 4 GB, which a single
  location's archive easily exceeds. File permissions and timestamps are
  stored inside the archives, so they are restored correctly even from an
  exFAT drive.
- Eject the drive in Finder (or with `diskutil eject /Volumes/MyDrive`) after
  the backup finishes, so all data is flushed to it.

## What a backup contains
```
/Volumes/MyDrive/macup/2026-10-20
├── config.yaml
├── Github-6f02e9e0….tar.gz
├── Documents-b7df298d….tar.gz
└── …
```

- `config.yaml` is a copy of the config the backup was created with. `restore`
  uses it to know which archives to extract and where.
- Each location is stored as a gzip-compressed tar archive, named after the
  location's directory and a hash of its configured path.

The archives are regular `.tar.gz` files, so you can also open them with
Finder or `tar` to grab a single file without restoring everything:

```sh
tar -xzf Github-6f02e9e0….tar.gz -C /tmp Github/app/README.md
```

## Restoring a backup
```sh
macup restore -b /Volumes/MyDrive/macup/2026-10-20
```

`restore` reads the `config.yaml` inside the backup and extracts every
location's archive back to its configured path, creating missing directories
along the way. It doesn't use your current config at all.

Restoring into a directory that already exists merges the backup into it:
files from the backup overwrite files with the same name, and files that are
not in the backup are left untouched.

## Backing up .env files
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

`--pattern` accepts shell-style wildcards (`*`, `?`, `[…]`) and replaces the
default patterns.

## Testing a restore with clear
```sh
macup clear      # asks you to type DELETE to confirm
macup clear -y   # skip the confirmation
```

`clear` permanently deletes every location listed in the config, so you can
check that a backup restores correctly. It refuses to delete top-level
directories and your home directory, but everything else in your locations is
gone afterwards. Only use it after verifying that the backup is complete, or
with a separate test config passed via `-c`.
