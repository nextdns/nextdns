#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/binary-size-diff.sh <base-ref> <head-ref> [target...]

Compare nextdns binary size between two git refs for major platforms.

Arguments:
  <base-ref>   Older/reference git ref (tag, branch, or commit)
  <head-ref>   Newer/compare git ref (tag, branch, or commit)
  [target...]  Optional target list in GOOS/GOARCH form

Default targets:
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64

Notes:
  - Builds use release-like flags: -trimpath and -ldflags='-s -w'.
  - CGO defaults follow release config:
    darwin => CGO_ENABLED=1, others => CGO_ENABLED=0.
  - Use from the repository root.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -lt 2 ]]; then
  usage
  exit 1
fi

if ! command -v git >/dev/null 2>&1 || ! command -v go >/dev/null 2>&1; then
  echo "error: git and go must be installed and available in PATH" >&2
  exit 1
fi

if [[ ! -d .git ]]; then
  echo "error: run this script from the repository root" >&2
  exit 1
fi

base_ref_input="$1"
head_ref_input="$2"
shift 2

base_ref="$(git rev-parse --verify "${base_ref_input}^{commit}")"
head_ref="$(git rev-parse --verify "${head_ref_input}^{commit}")"

if [[ $# -gt 0 ]]; then
  targets=("$@")
else
  targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
  )
fi

tmpdir="$(mktemp -d)"
base_wt="${tmpdir}/base"
head_wt="${tmpdir}/head"

cleanup() {
  git worktree remove --force "${base_wt}" >/dev/null 2>&1 || true
  git worktree remove --force "${head_wt}" >/dev/null 2>&1 || true
  rm -rf "${tmpdir}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

git worktree add --detach "${base_wt}" "${base_ref}" >/dev/null
git worktree add --detach "${head_wt}" "${head_ref}" >/dev/null

size_of_build() {
  local wt="$1"
  local target="$2"
  local goos goarch ext out build_log cgo_enabled err_line

  IFS='/' read -r goos goarch _ <<<"${target}"
  if [[ -z "${goos:-}" || -z "${goarch:-}" ]]; then
    echo "invalid target: ${target}" >&2
    return 2
  fi

  ext=""
  if [[ "${goos}" == "windows" ]]; then
    ext=".exe"
  fi

  out="${wt}/.size-build/nextdns-${goos}-${goarch}${ext}"
  mkdir -p "${wt}/.size-build"
  build_log="${wt}/.size-build/build-${goos}-${goarch}.log"
  cgo_enabled=0
  if [[ "${goos}" == "darwin" ]]; then
    cgo_enabled=1
  fi

  if ! (
    cd "${wt}"
    CGO_ENABLED="${cgo_enabled}" GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags="-s -w" -o "${out}" .
  ) >"${build_log}" 2>&1; then
    err_line="$(awk 'NF && $0 !~ /^#/ { print; exit }' "${build_log}")"
    if [[ -z "${err_line}" ]]; then
      err_line="$(awk 'NF { print; exit }' "${build_log}")"
    fi
    echo "build error (${target}): ${err_line}" >&2
    return 1
  fi

  wc -c < "${out}" | tr -d '[:space:]'
}

humanize_bytes() {
  awk -v bytes="$1" 'BEGIN {
    split("B KiB MiB GiB TiB", units, " ")
    size = bytes + 0
    unit = 1
    while (size >= 1024 && unit < 5) {
      size /= 1024
      unit++
    }
    if (unit == 1) printf "%d %s", size, units[unit]
    else printf "%.2f %s", size, units[unit]
  }'
}

printf "Comparing binary sizes\n"
printf "  base: %s (%s)\n" "${base_ref_input}" "${base_ref:0:12}"
printf "  head: %s (%s)\n\n" "${head_ref_input}" "${head_ref:0:12}"

printf "%-15s %12s %12s %14s %10s\n" "TARGET" "BASE" "HEAD" "DELTA" "CHANGE"
printf "%-15s %12s %12s %14s %10s\n" "------" "----" "----" "-----" "------"

failed=0
for target in "${targets[@]}"; do
  base_size=""
  head_size=""

  if ! base_size="$(size_of_build "${base_wt}" "${target}")"; then
    printf "%-15s %12s %12s %14s %10s\n" "${target}" "BUILD-FAIL" "-" "-" "-"
    failed=1
    continue
  fi

  if ! head_size="$(size_of_build "${head_wt}" "${target}")"; then
    printf "%-15s %12s %12s %14s %10s\n" "${target}" "-" "BUILD-FAIL" "-" "-"
    failed=1
    continue
  fi

  delta=$((head_size - base_size))
  if [[ "${delta}" -ge 0 ]]; then
    delta_sign="+"
  else
    delta_sign=""
  fi

  pct="$(awk -v b="${base_size}" -v d="${delta}" 'BEGIN {
    if (b == 0) { print "n/a"; exit }
    printf "%+.2f%%", (d / b) * 100
  }')"

  printf "%-15s %12s %12s %14s %10s\n" \
    "${target}" \
    "$(humanize_bytes "${base_size}")" \
    "$(humanize_bytes "${head_size}")" \
    "${delta_sign}$(humanize_bytes "${delta#-}")" \
    "${pct}"
done

if [[ "${failed}" -ne 0 ]]; then
  echo
  echo "warning: one or more targets failed to build" >&2
  exit 2
fi
