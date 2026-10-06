#!/usr/bin/env bash
# Build and install local source without changing the operator configuration.
set +x
set -Eeuo pipefail
umask 077

fail() {
  printf 'install-local: %s\n' "$*" >&2
  exit 1
}

[[ $# == 2 ]] || fail 'Use make install-local [AK3S_CONFIG=/path/to/values.yaml].'
default_image=$1
operator_config=$2
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
cli="$repo_root/bin/ak3s"
[[ -x "$cli" ]] || fail 'Build the CLI with make build first.'
for tool in docker awk mktemp flock uname install; do
  command -v "$tool" >/dev/null || fail "Required command is missing: $tool"
done
case "$(uname -sm)" in
  'Linux x86_64') architecture=amd64 ;;
  'Linux aarch64') architecture=arm64 ;;
  *) fail 'Run this helper on the Linux amd64 or arm64 test server.' ;;
esac

exec 9<"${BASH_SOURCE[0]}"
flock -n 9 || fail 'Another local installation is running from this checkout.'
as_root=()
if (( EUID != 0 )); then
  command -v sudo >/dev/null || fail 'sudo is required to install the local cluster.'
  sudo -v
  as_root=(sudo --)
fi

[[ ! -L "$repo_root/.tmp" ]] || fail 'The temporary directory must not be a symlink.'
mkdir -p -- "$repo_root/.tmp"
work_dir=$(mktemp -d "$repo_root/.tmp/install-local-XXXXXX")
# kubectl can create root-owned discovery cache directories under work_dir.
trap '"${as_root[@]}" rm -rf -- "$work_dir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# Parse only canonical CLI output, never the operator's hand-written YAML.
# Keep the complete configuration private, including any Helm overrides.
"${as_root[@]}" "$cli" config --config "$operator_config" > "$work_dir/effective.yaml"
awk '
  /^platform: true$/ { enabled = 1 }
  END { exit !enabled }
' "$work_dir/effective.yaml" || fail 'Set platform: true in the operator configuration once before using this helper.'
image=$(awk '/^dashboard_image: / { print substr($0, 18) }' "$work_dir/effective.yaml")
image=${image#\"}
image=${image%\"}
image=${image#\'}
image=${image%\'}
image=${image:-$default_image}
[[ "$image" =~ ^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] ||
  fail 'A local dashboard build needs an image tag; use an empty dashboard_image or a tagged reference instead of a digest.'
awk '/^platform: true$/ { print "platform: false"; next } { print }' \
  "$work_dir/effective.yaml" > "$work_dir/bootstrap.yaml"

printf 'Validate the full configuration and existing AK3S installation.\n'
"${as_root[@]}" "$cli" install --config "$operator_config" --dry-run

printf 'Build the dashboard image for linux/%s.\n' "$architecture"
docker build --platform "linux/$architecture" -f "$repo_root/dashboard/Dockerfile" -t "$image" "$repo_root"

printf 'Ensure K3s is ready using a temporary bootstrap configuration.\n'
"${as_root[@]}" "$cli" install --config "$work_dir/bootstrap.yaml"

printf 'Transfer the dashboard image directly from Docker into K3s.\n'
docker image save "$image" | "${as_root[@]}" k3s ctr --address /run/k3s/containerd/containerd.sock --namespace k8s.io images import -

# Restart an existing dashboard before apply waits for it. This also recovers a
# failed rollout or replaces an older image built with the same local tag.
kubectl=("${as_root[@]}" k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml --cache-dir "$work_dir/kube-cache")
existing=$("${kubectl[@]}" -n ak3s get deployment dashboard --ignore-not-found -o name --request-timeout=30s)
if [[ -n "$existing" ]]; then
  "${kubectl[@]}" -n ak3s rollout restart deployment/dashboard --request-timeout=30s
fi

printf 'Install the platform using the original operator configuration.\n'
"${as_root[@]}" "$cli" apply --config "$operator_config"
"${as_root[@]}" install -m 0755 "$cli" /usr/local/bin/ak3s
printf 'Local installation complete. Run make install-local again after source changes.\n'
