#!/usr/bin/env bash
# Download one released binary. Host/cluster orchestration lives entirely in Go.
set -euo pipefail
version="${1:-}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || [[ "$#" != 1 ]]; then
  echo 'Usage: bash install.sh vX.Y.Z' >&2
  exit 2
fi
[[ "$(uname -s)" == Linux ]] || { echo 'Only Linux is supported.' >&2; exit 1; }
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Only amd64 and arm64 are supported.' >&2; exit 1 ;;
esac
for tool in curl sha256sum mktemp install; do command -v "$tool" >/dev/null; done
destination="${AK3S_INSTALL_DIR:-/usr/local/bin}"
asset="ak3s_linux_$arch"
base="https://github.com/jgador/ak3s/releases/download/$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --retry 3 "$base/$asset" -o "$tmp/$asset"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --retry 3 "$base/checksums.txt" -o "$tmp/checksums.txt"
# Accept exactly one entry for this asset; never let a checksum file name paths.
expected=''
while read -r sum name rest; do
  if [[ "$name" == "$asset" ]]; then
    [[ -z "$expected" && -z "$rest" && "$sum" =~ ^[a-fA-F0-9]{64}$ ]] || { echo 'Invalid checksum manifest.' >&2; exit 1; }
    expected="$sum"
  fi
done < "$tmp/checksums.txt"
[[ -n "$expected" ]] || { echo 'Release checksum is missing.' >&2; exit 1; }
printf '%s  %s\n' "$expected" "$asset" > "$tmp/selected.sha256"
(cd "$tmp" && sha256sum --check --status selected.sha256)
mkdir -p "$destination"
# Stage on the destination filesystem before replacing an existing CLI.
staged="$(mktemp "$destination/.ak3s-XXXXXX")"
trap 'rm -rf "$tmp"; rm -f "$staged"' EXIT
install -m 0755 "$tmp/$asset" "$staged"
mv -f "$staged" "$destination/ak3s"
printf 'Installed ak3s %s to %s/ak3s\n' "$version" "$destination"
