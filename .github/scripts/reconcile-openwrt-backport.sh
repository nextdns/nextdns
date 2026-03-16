#!/usr/bin/env bash

set -euo pipefail

target_branch=${1:?usage: reconcile-openwrt-backport.sh <openwrt-branch>}
pkg_path="net/nextdns/Makefile"
stable_series=${target_branch#openwrt-}
backport_branch="nextdns-backport-${target_branch}"
backport_ref="origin/${backport_branch}"

require_clean_repo() {
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "Working tree is not clean" >&2
        exit 1
    fi
}

version_from_stream() {
    sed -n 's/^PKG_VERSION:=//p' | head -n1
}

version_from_ref() {
    git show "$1:$pkg_path" | version_from_stream
}

set_output() {
    if [ -z "${GITHUB_OUTPUT:-}" ]; then
        return 0
    fi
    cat >>"$GITHUB_OUTPUT"
}

git fetch --no-tags origin master "$target_branch" "$backport_branch" 2>/dev/null || git fetch --no-tags origin master "$target_branch"
git checkout -B "$target_branch" "origin/$target_branch"
git reset --hard "origin/$target_branch"
require_clean_repo

stable_version=$(version_from_ref "HEAD")
master_version=$(version_from_ref "origin/master")

if [ -z "$stable_version" ] || [ -z "$master_version" ]; then
    echo "Unable to determine OpenWrt package versions" >&2
    exit 1
fi

if [ "$stable_version" = "$master_version" ]; then
    set_output <<EOF
has_changes=false
stable_version=$stable_version
master_version=$master_version
EOF
    exit 0
fi

mapfile -t history < <(git log --reverse --format='%H' origin/master -- "$pkg_path")

found_stable=0
fallback_mode=0
commits=()
for sha in "${history[@]}"; do
    version=$(version_from_ref "$sha")
    [ -n "$version" ] || continue
    if [ "$found_stable" -eq 0 ]; then
        if [ "$version" = "$stable_version" ]; then
            found_stable=1
        fi
        continue
    fi
    commits+=("$sha")
done

if [ "$found_stable" -eq 0 ]; then
    fallback_mode=1
    latest_sha=$(git log -1 --format='%H' origin/master -- "$pkg_path")
    commits=("$latest_sha")
fi

if [ "${#commits[@]}" -eq 0 ]; then
    set_output <<EOF
has_changes=false
stable_version=$stable_version
master_version=$master_version
EOF
    exit 0
fi

applied=()
for sha in "${commits[@]}"; do
    if git cherry-pick --signoff -x "$sha"; then
        applied+=("$sha")
        continue
    fi

    if [ -n "$(git diff --name-only --diff-filter=U)" ]; then
        echo "Cherry-pick conflict while applying $sha to $target_branch" >&2
        exit 1
    fi

    git cherry-pick --skip
done

if [ "$(git rev-list --count "origin/$target_branch"..HEAD)" -eq 0 ]; then
    set_output <<EOF
has_changes=false
stable_version=$stable_version
master_version=$master_version
EOF
    exit 0
fi

if git show-ref --verify --quiet "refs/remotes/${backport_ref}"; then
    if [ "$(git rev-parse HEAD^{tree})" = "$(git rev-parse ${backport_ref}^{tree})" ]; then
        backport_version=$(version_from_ref "$backport_ref")
        set_output <<EOF
has_changes=false
stable_version=$stable_version
master_version=$master_version
backport_version=$backport_version
existing_backport_branch=$backport_branch
EOF
        exit 0
    fi
fi

backport_version=$(version_from_ref "HEAD")
if [ "$backport_version" = "$stable_version" ]; then
    pr_title="[$stable_series] nextdns: backport master changes"
else
    pr_title="[$stable_series] nextdns: update to version $backport_version"
fi

details=""
if [ "$backport_version" != "$stable_version" ]; then
    details=$(printf 'Current branch version: `%s`\nTarget branch version: `%s`\n' "$stable_version" "$backport_version")
fi

commit_list=$(for sha in "${applied[@]}"; do
    printf -- '- %s\n' "$sha"
done)

fallback_note=""
if [ "$fallback_mode" -eq 1 ]; then
    fallback_note=$(printf '\n_Fallback mode was used because `%s` was not found in `master` history._\n' "$stable_version")
fi

set_output <<EOF
has_changes=true
stable_version=$stable_version
master_version=$master_version
backport_version=$backport_version
backport_branch=$backport_branch
pr_title=$pr_title
pr_body<<BODY
Backport merged \`nextdns\` package changes from \`master\` into \`$target_branch\`.

$details
Cherry picked from commits:
$commit_list$fallback_note
BODY
EOF
