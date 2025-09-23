# macup
A Go-powered backup &amp; restore tool for macOS. Define folders, excludes,
apps, dev tools, and system tweaks in a central YAML config. Create clean
backups, auto-install essentials via Homebrew/App Store, restore dotfiles, and
reapply macOS settings for a consistent setup.

## Roadmap
- [] Create a backup according to the given configuration
- [] Restore all files, settings and programs from a created backup
- [] Implement a user-friendly TUI for configuring a backup
- [] Support for incremental backups
- [] Add synchronization with a remote file storage

## Feature Ideas
- [] Optimize backup performance by detecting compressabilty of certain file
     types and only compress files that are not already compressed (e.g. JPEG)
