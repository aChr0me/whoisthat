#!/usr/bin/env bash
# Link this plugin into Omarchy's plugins directory so edits in the repo are
# live. Idempotent; refuses to replace a real directory. Use --uninstall to
# remove the link.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
id="io.github.achr0me.whoisthat"
dest_dir="${XDG_CONFIG_HOME:-$HOME/.config}/omarchy/plugins"
dest="$dest_dir/$id"

if [[ "${1:-}" == "--uninstall" ]]; then
    if [[ -L "$dest" ]]; then rm "$dest" && echo "removed $dest"; else echo "no link at $dest"; fi
    exit 0
fi

mkdir -p "$dest_dir"
if [[ -L "$dest" ]]; then
    ln -sfn "$here" "$dest"
elif [[ -e "$dest" ]]; then
    echo "error: $dest exists and is not a symlink (an old copy?); move it away first" >&2
    exit 1
else
    ln -s "$here" "$dest"
fi
echo "linked $dest -> $here"

if command -v omarchy-shell >/dev/null 2>&1; then
    omarchy-shell shell rescanPlugins || echo "rescan failed; restart the shell instead" >&2
fi
