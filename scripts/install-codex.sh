#!/bin/sh
# Build-stage only. Pin the official standalone static-musl CLI, not npm wrappers.
set -eu
arch=${1:?usage: install-codex.sh TARGETARCH OUTPUT_ROOT}
root=${2:?usage: install-codex.sh TARGETARCH OUTPUT_ROOT}
version=0.167.2
case "$arch" in
  amd64) target=x86_64-unknown-linux-musl; sha=8246e846c550ea21197fcc4067bc270dca895e94ea42ed6a282e6f4e0125b15f ;;
  arm64) target=aarch64-unknown-linux-musl; sha=ed81fa2c7d209689af061b6f305ed554218de92a1c2c4019142b28f66fa64add ;;
  *) echo "Unsupported Codex architecture: $arch" >&2; exit 1 ;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
fetch() { curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "$1" -o "$2"; }
fetch "https://github.com/openai/codex/releases/download/rust-v${version}/codex-${target}.tar.gz" "$work/codex.tar.gz"
printf '%s  %s\n' "$sha" "$work/codex.tar.gz" | sha256sum -c -
tar -xzf "$work/codex.tar.gz" -C "$work" "codex-${target}"
mkdir -p "$root/usr/local/bin" "$root/usr/share/licenses/codex"
install -m 0755 "$work/codex-${target}" "$root/usr/local/bin/codex"
# The standalone archive has no license files; preserve the pinned upstream notices.
for name in LICENSE NOTICE; do
  fetch "https://raw.githubusercontent.com/openai/codex/rust-v${version}/${name}" "$work/$name"
done
printf '%s  %s\n' \
  d17f227e4df5da1600391338865ce0f3055211760a36688f816941d58232d8dc "$work/LICENSE" \
  9d71575ecfd9a843fc1677b0efb08053c6ba9fd686a0de1a6f5382fd3c220915 "$work/NOTICE" | sha256sum -c -
install -m 0644 "$work/LICENSE" "$work/NOTICE" "$root/usr/share/licenses/codex/"
printf 'Codex %s\nTarget: %s\nArchive SHA256: %s\n' "$version" "$target" "$sha" > "$root/usr/share/licenses/codex/BUILD-INFO"
