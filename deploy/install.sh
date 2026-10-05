#!/usr/bin/env bash
# shed installer.
#
#   curl -fsSL https://github.com/kyledickey/shed/releases/latest/download/install.sh | sudo bash
#
# Installs the requirements (Docker with buildx, git, railpack), downloads and
# verifies the latest shed release, writes /etc/shed/shed.toml, and starts the
# systemd service. On a host that already has /etc/shed/shed.toml it upgrades
# the binary and unit and keeps the config.
#
# Every prompt has an environment variable, so it also runs unattended:
#
#   SHED_DOMAIN          dashboard domain, e.g. shed.example.com (required)
#   SHED_ALLOWED_USERS   comma-separated GitHub logins allowed to sign in (required)
#   SHED_BASE_DOMAIN     base domain for generated app domains (optional)
#   SHED_ACME_EMAIL      Let's Encrypt contact email (optional)
#   SHED_VERSION         release to install, e.g. v1.2.3 (default: latest)
#   SHED_YES=1           never prompt, even when a terminal is available
#
# bash reads this script from stdin when piped, so everything lives in
# functions, main is called on the last line, and every interactive command
# reads from /dev/tty.

set -euo pipefail

readonly REPO="kyledickey/shed"
readonly MINISIGN_PUBLIC_KEY="RWRBeRUdMkDg7PI7SQWikYL/5Evv0feGGQ07ZbVIfV5Q80snhMX7QaMK"
readonly GUM_VERSION="2.0.2"
readonly GUM_SHA256_X86_64="d842e06d93dbed90af48cb8dd10698db6f22e331fc40346bb37bbc753109edc2"
readonly GUM_SHA256_ARM64="8ebf8b54ec1e8c81f2bb58b59ff9b70998186a4d11375f0cf357b80e0ccfa1d5"

readonly BIN_PATH="/usr/local/bin/shed"
readonly UNIT_PATH="/etc/systemd/system/shed.service"
readonly CONFIG_DIR="/etc/shed"
readonly CONFIG_FILE="$CONFIG_DIR/shed.toml"
readonly LOG_FILE="$CONFIG_DIR/shed.log"

readonly ACCENT="#3ecf72"

# Set by main and the steps below.
TMP=""
ARCH=""      # amd64 or arm64
GUM_ARCH=""  # x86_64 or arm64
INTERACTIVE=0
GUM=0
TAG=""
ARCHIVE=""
SETUP_URL=""
SETUP_TOKEN=""

# --- output ---------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

say() {
  if [ "$GUM" = 1 ]; then gum style "$*"; else printf '%s\n' "$*"; fi
}

note() {
  if [ "$GUM" = 1 ]; then gum style --faint "$*"; else printf '%s\n' "$*"; fi
}

step() {
  if [ "$GUM" = 1 ]; then
    gum style --bold --foreground "$ACCENT" "$*"
  else
    printf '==> %s\n' "$*"
  fi
}

warn() {
  if [ "$GUM" = 1 ]; then
    gum style --foreground 214 "warning: $*" >&2
  else
    printf 'warning: %s\n' "$*" >&2
  fi
}

die() {
  if [ "$GUM" = 1 ]; then
    gum style --foreground 196 "error: $*" >&2
  else
    printf 'error: %s\n' "$*" >&2
  fi
  exit 1
}

# box prints its arguments, one per line, in a bordered box.
box() {
  if [ "$GUM" = 1 ]; then
    gum style --border rounded --border-foreground "$ACCENT" --padding "1 2" --margin "1 0" "$@"
  else
    printf '\n'
    printf '  %s\n' "$@"
    printf '\n'
  fi
}

# --- prompts --------------------------------------------------------------

# confirm asks a yes/no question. It always succeeds when unattended.
confirm() {
  [ "$INTERACTIVE" = 1 ] || return 0
  gum confirm "$1" --default="${2:-true}" \
    --prompt.foreground "$ACCENT" --selected.background "$ACCENT" --selected.foreground 0 \
    </dev/tty
}

# input prints the line the user typed. Usage: input HEADER VALUE PLACEHOLDER.
input() {
  gum input --header "$1" --value "$2" --placeholder "$3" --prompt "> " --width 60 \
    --header.foreground "$ACCENT" --prompt.foreground "$ACCENT" --cursor.foreground "$ACCENT" \
    </dev/tty
}

# spin runs a command behind a spinner (or plainly when unattended). The command
# must be an external program, not a shell function. It returns the command's
# status. Usage: spin TITLE CMD...
spin() {
  local title=$1
  shift
  if [ "$GUM" = 1 ]; then
    gum spin --title "$title" --spinner.foreground "$ACCENT" --show-error -- \
      bash -c 'exec "$@" </dev/null' _ "$@" </dev/tty
  else
    step "$title"
    "$@" </dev/null
  fi
}

# --- validation -----------------------------------------------------------
# Validators print the normalized value and succeed, or fail.

readonly LABEL='[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?'
readonly HOST_RE="^(${LABEL}\\.)+${LABEL}\$"
readonly LOGIN_RE='^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$'
readonly EMAIL_RE='^[^@[:space:][:cntrl:]]+@[^@[:space:][:cntrl:]]+\.[^@[:space:][:cntrl:]]+$'

valid_domain() {
  local v=${1,,}
  v=${v#http://}
  v=${v#https://}
  v=${v%%/*}
  v=${v%.}
  [ "${#v}" -le 253 ] || return 1
  [[ $v =~ $HOST_RE ]] || return 1
  [[ ${v##*.} =~ ^[0-9]+$ ]] && return 1
  printf '%s' "$v"
}

valid_email() {
  [[ $1 =~ $EMAIL_RE ]] || return 1
  printf '%s' "$1"
}

# valid_users normalizes a comma or space separated list to "a,b,c".
valid_users() {
  local raw=${1//,/ } login out=""
  for login in $raw; do
    login=${login#@}
    [[ $login =~ $LOGIN_RE ]] || return 1
    out+="${out:+,}$login"
  done
  [ -n "$out" ] || return 1
  printf '%s' "$out"
}

# toml_quote prints a TOML basic string.
toml_quote() {
  local s=${1//\\/\\\\}
  s=${s//\"/\\\"}
  printf '"%s"' "$s"
}

# ask sets the variable named VAR from the matching prompt, or from its
# environment value when unattended. Usage:
#   ask VAR HEADER PLACEHOLDER VALIDATOR HINT REQUIRED
ask() {
  local var=$1 header=$2 placeholder=$3 validate=$4 hint=$5 required=$6
  local value=${!var:-} out
  while :; do
    if [ "$INTERACTIVE" = 1 ]; then
      value=$(input "$header" "$value" "$placeholder") || die "Cancelled."
    fi
    if [ -z "$value" ] && [ "$required" = 0 ]; then
      printf -v "$var" ''
      return 0
    fi
    if [ -n "$value" ] && out=$("$validate" "$value"); then
      printf -v "$var" '%s' "$out"
      return 0
    fi
    if [ "$INTERACTIVE" = 1 ]; then
      warn "$hint"
    else
      die "$var: invalid value '$value'. $hint"
    fi
  done
}

# --- preflight ------------------------------------------------------------

preflight() {
  [ "$(uname -s)" = Linux ] || die "shed runs on Linux only."
  case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 GUM_ARCH=x86_64 ;;
    aarch64 | arm64) ARCH=arm64 GUM_ARCH=arm64 ;;
    *) die "Unsupported architecture $(uname -m); shed supports amd64 and arm64." ;;
  esac
  if [ "$(id -u)" -ne 0 ]; then
    die "Run as root: curl -fsSL https://github.com/$REPO/releases/latest/download/install.sh | sudo bash"
  fi
  if ! have systemctl || [ ! -d /run/systemd/system ]; then
    die "systemd is required."
  fi
  local cmd
  for cmd in curl tar sha256sum; do
    have "$cmd" || die "$cmd is required but not installed."
  done
}

# detect_mode decides between prompting and unattended mode.
detect_mode() {
  INTERACTIVE=0
  if [ "${SHED_YES:-}" != 1 ] && { : </dev/tty; } 2>/dev/null; then
    INTERACTIVE=1
  fi
}

# bootstrap_gum puts gum on PATH, downloading a pinned release when missing.
bootstrap_gum() {
  if have gum; then
    GUM=1
    return 0
  fi
  local want url dir bin
  case "$GUM_ARCH" in
    x86_64) want=$GUM_SHA256_X86_64 ;;
    *) want=$GUM_SHA256_ARM64 ;;
  esac
  url="https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/gum_${GUM_VERSION}_Linux_${GUM_ARCH}.tar.gz"
  dir="$TMP/gum"
  mkdir -p "$dir"
  printf 'Downloading gum %s...\n' "$GUM_VERSION"
  curl -fsSL --retry 3 -o "$TMP/gum.tar.gz" "$url" || die "Could not download gum from $url"
  printf '%s  %s\n' "$want" "$TMP/gum.tar.gz" | sha256sum -c --status - ||
    die "gum checksum mismatch; refusing to run it."
  tar -xzf "$TMP/gum.tar.gz" -C "$dir"
  bin=$(find "$dir" -type f -name gum | head -n 1)
  [ -n "$bin" ] || die "gum binary not found in its archive."
  chmod 0755 "$bin"
  PATH="$(dirname "$bin"):$PATH"
  export PATH
  GUM=1
}

header() {
  if [ "$GUM" = 1 ]; then
    gum style --bold --foreground "$ACCENT" --margin "1 0 0 0" "shed"
    gum style --faint "Self-hosted deployments on a single server."
    printf '\n'
  else
    printf 'shed installer\n'
  fi
}

# --- release --------------------------------------------------------------

resolve_version() {
  local url
  if [ -n "${SHED_VERSION:-}" ]; then
    TAG="v${SHED_VERSION#v}"
  else
    url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
      die "Could not find the latest release at github.com/$REPO. Check your network, or set SHED_VERSION."
    TAG=${url##*/}
  fi
  [[ $TAG =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
    die "Could not determine a release version (got '$TAG'). Set SHED_VERSION, e.g. SHED_VERSION=v1.2.3."
  ARCHIVE="shed_${TAG#v}_linux_${ARCH}.tar.gz"
}

download_release() {
  local base="https://github.com/$REPO/releases/download/$TAG"
  local args=(-fsSL --retry 3
    -o "$TMP/$ARCHIVE" "$base/$ARCHIVE"
    -o "$TMP/checksums.txt" "$base/checksums.txt")
  if have minisign; then
    args+=(-o "$TMP/checksums.txt.minisig" "$base/checksums.txt.minisig")
  fi
  spin "Downloading shed $TAG" curl "${args[@]}" ||
    die "Could not download $ARCHIVE from github.com/$REPO (does release $TAG exist?)."
}

verify_release() {
  local want got
  if have minisign; then
    minisign -V -m "$TMP/checksums.txt" -P "$MINISIGN_PUBLIC_KEY" >/dev/null ||
      die "Signature verification of checksums.txt failed; refusing to install."
    note "Signature verified."
  else
    note "minisign not found; signature not checked (shed verifies signatures itself for in-app updates)."
  fi
  want=$(awk -v f="$ARCHIVE" '$2 == f || $2 == "*" f { print $1; exit }' "$TMP/checksums.txt")
  [ -n "$want" ] || die "checksums.txt has no entry for $ARCHIVE."
  got=$(sha256sum "$TMP/$ARCHIVE" | awk '{ print $1 }')
  [ "$want" = "$got" ] || die "Checksum mismatch for $ARCHIVE (expected $want, got $got)."
  note "Checksum verified."
}

# install_release installs the binary and unit, keeping a replaced binary as
# shed.prev for rolling back, like in-app updates do.
install_release() {
  local stage="$TMP/stage"
  mkdir -p "$stage"
  tar -xzf "$TMP/$ARCHIVE" -C "$stage" shed shed.service
  [ ! -f "$BIN_PATH" ] || cp -f "$BIN_PATH" "$BIN_PATH.prev"
  install -m 0755 "$stage/shed" "$BIN_PATH"
  install -m 0644 "$stage/shed.service" "$UNIT_PATH"
}

# --- requirements ---------------------------------------------------------

# pkg_install installs packages with the host's package manager.
pkg_install() {
  if have apt-get; then
    spin "Installing $*" bash -c \
      'export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a; apt-get update -qq && apt-get install -y -qq "$@"' _ "$@"
  elif have dnf; then
    spin "Installing $*" dnf install -y "$@"
  elif have yum; then
    spin "Installing $*" yum install -y "$@"
  elif have zypper; then
    spin "Installing $*" zypper --non-interactive install "$@"
  elif have pacman; then
    spin "Installing $*" pacman -Sy --noconfirm --needed "$@"
  elif have apk; then
    spin "Installing $*" apk add --no-cache "$@"
  else
    return 1
  fi
}

buildx_package() {
  if have pacman; then
    echo docker-buildx
  elif have apk; then
    echo docker-cli-buildx
  else
    echo docker-buildx-plugin
  fi
}

missing_requirements() {
  local m=()
  have docker || m+=(Docker)
  have git || m+=(git)
  have railpack || m+=(railpack)
  local IFS=,
  printf '%s' "${m[*]}" | sed 's/,/, /g'
}

ensure_requirements() {
  if ! have docker; then
    spin "Installing Docker" bash -c 'set -o pipefail; curl -fsSL https://get.docker.com | sh' ||
      die "Docker installation failed. Install Docker Engine manually and re-run."
  fi
  systemctl enable --now docker >/dev/null 2>&1 ||
    die "Could not start docker.service. Check: systemctl status docker"

  if ! docker buildx version >/dev/null 2>&1; then
    pkg_install "$(buildx_package)" || true
    docker buildx version >/dev/null 2>&1 ||
      die "The Docker buildx plugin is missing. Install it (package docker-buildx-plugin, see https://docs.docker.com/build/install-buildx/) and re-run."
  fi

  if ! have git; then
    pkg_install git || die "Could not install git. Install it and re-run."
  fi

  if ! have railpack; then
    spin "Installing railpack" bash -c 'set -o pipefail; curl -fsSL https://railpack.com/install.sh | bash -s -- --yes' ||
      die "railpack installation failed. Install it from https://railpack.com and re-run."
    have railpack || die "railpack is not on PATH after installing. Install it from https://railpack.com and re-run."
  fi
}

# --- questions ------------------------------------------------------------

check_dns() {
  local domain=$1 resolved host_ips ip
  resolved=$(getent ahosts "$domain" 2>/dev/null | awk '{ print $1 }' | sort -u | tr '\n' ' ') || true
  if [ -z "${resolved// /}" ]; then
    warn "$domain does not resolve yet. Create an A (and AAAA) record pointing at this server, or HTTPS certificates will fail."
    confirm "Continue anyway?" || die "Aborted."
    return 0
  fi
  host_ips=" $(hostname -I 2>/dev/null || true) "
  for ip in $resolved; do
    [[ $host_ips == *" $ip "* ]] && return 0
  done
  resolved=${resolved% }
  warn "$domain resolves to ${resolved// /, }, which is not an address of this host. That is expected behind NAT or a proxy; otherwise point the record at this server."
  confirm "Continue anyway?" || die "Aborted."
}

check_ports() {
  have ss || return 0
  local port
  for port in 80 443; do
    if ss -Hltn | awk -v p=":$port" '$4 ~ p "$" { found = 1 } END { exit !found }'; then
      warn "Port $port is already in use; shed's proxy needs it. Stop the other service (nginx, apache, caddy) first."
      confirm "Continue anyway?" "false" || die "Aborted."
    fi
  done
}

ask_missing_env() {
  [ "$INTERACTIVE" = 0 ] || return 0
  local missing=()
  [ -n "${SHED_DOMAIN:-}" ] || missing+=(SHED_DOMAIN)
  [ -n "${SHED_ALLOWED_USERS:-}" ] || missing+=(SHED_ALLOWED_USERS)
  [ "${#missing[@]}" -eq 0 ] ||
    die "Unattended install needs these environment variables: ${missing[*]}"
}

collect_answers() {
  ask_missing_env

  [ "$INTERACTIVE" = 0 ] || note "The hostname you will open shed at, e.g. shed.example.com. Its DNS record must point at this server."
  ask SHED_DOMAIN "Dashboard domain" "shed.example.com" valid_domain \
    "Enter a hostname such as shed.example.com." 1
  check_dns "$SHED_DOMAIN"

  if [ "$INTERACTIVE" = 1 ]; then
    SHED_BASE_DOMAIN=${SHED_BASE_DOMAIN-apps.$SHED_DOMAIN}
    note "Apps get <service>-<project>.<base domain>. This needs a wildcard DNS record *.<base domain> pointing at this server. Leave empty to disable generated domains."
  fi
  ask SHED_BASE_DOMAIN "Apps base domain (optional)" "apps.example.com" valid_domain \
    "Enter a hostname such as apps.example.com, or leave it empty." 0

  [ "$INTERACTIVE" = 0 ] || note "Used for Let's Encrypt expiry notices. Optional."
  ask SHED_ACME_EMAIL "ACME email (optional)" "you@example.com" valid_email \
    "Enter an email address, or leave it empty." 0

  [ "$INTERACTIVE" = 0 ] || note "Comma-separated. Only these accounts can sign in; there is no automatic first-user enrollment."
  ask SHED_ALLOWED_USERS "GitHub logins allowed to sign in" "octocat,hubot" valid_users \
    "Enter one or more GitHub logins, separated by commas." 1
}

show_summary() {
  local base="disabled" email="none" extra lines
  [ -z "$SHED_BASE_DOMAIN" ] || base="*.$SHED_BASE_DOMAIN"
  [ -z "$SHED_ACME_EMAIL" ] || email=$SHED_ACME_EMAIL
  extra=$(missing_requirements)
  lines=(
    "Version        $TAG"
    "Dashboard      https://$SHED_DOMAIN"
    "App domains    $base"
    "ACME email     $email"
    "Allowed users  ${SHED_ALLOWED_USERS//,/, }"
  )
  [ -z "$extra" ] || lines+=("Also installing $extra")
  box "${lines[@]}"
  confirm "Install shed with these settings?" || die "Aborted."
}

# --- config and service ---------------------------------------------------

write_config() {
  install -d -m 0755 "$CONFIG_DIR"
  local users="" login
  for login in ${SHED_ALLOWED_USERS//,/ }; do
    users+="${users:+, }$(toml_quote "$login")"
  done
  (
    umask 077
    cat >"$CONFIG_FILE" <<EOF
# Written by deploy/install.sh. Every key can also be set with a SHED_*
# environment variable, e.g. SHED_SERVER_URL.

[server]
listen = "127.0.0.1:3000"
url = $(toml_quote "https://$SHED_DOMAIN")

[data]
dir = "/var/lib/shed"

[proxy]
enabled = true
http_port = 80
https_port = 443
acme_email = $(toml_quote "$SHED_ACME_EMAIL")
# Services get <service>-<project>.<base_domain>. Needs a wildcard DNS record.
base_domain = $(toml_quote "$SHED_BASE_DOMAIN")

[auth]
allowed_users = [$users]

[build]
memory_mb = 2048
cpus = 2
min_free_mb = 2048

[deployments]
log_max_mb = 10
keep = 50

[log]
level = "info"
EOF
  )
  chmod 0600 "$CONFIG_FILE"
}

show_journal() {
  journalctl -u shed -n 50 --no-pager >&2 || true
}

# find_setup_line prints the last setup-token log line written after the first
# $1 lines of the log file, falling back to the journal.
find_setup_line() {
  local skip=$1 line=""
  if [ -r "$LOG_FILE" ]; then
    line=$(tail -n +"$((skip + 1))" "$LOG_FILE" | grep 'setup token' | tail -n 1 || true)
  fi
  if [ -z "$line" ]; then
    line=$(journalctl -u shed --no-pager -o cat -n 200 2>/dev/null | grep 'setup token' | tail -n 1 || true)
  fi
  printf '%s' "$line"
}

# restart_count prints how many times systemd has restarted shed on its own.
restart_count() {
  local n
  n=$(systemctl show -p NRestarts --value shed 2>/dev/null) || n=0
  printf '%s' "${n:-0}"
}

# wait_for_shed waits up to 30 seconds for the service to stay up; $1 is
# restart_count from before it was started. With a log offset in $2 it also
# waits for the setup token and sets SETUP_URL and SETUP_TOKEN. Without one it
# only checks that the service is running.
wait_for_shed() {
  local base=$1 skip=${2:-} i line
  step "Waiting for shed to start"
  for ((i = 0; i < 30; i++)); do
    sleep 1
    if [ "$(restart_count)" != "$base" ]; then
      show_journal
      die "shed is crashing on start. See the log above, or run: journalctl -u shed -n 100"
    fi
    systemctl is-active --quiet shed || continue
    if [ -z "$skip" ]; then
      [ "$i" -ge 3 ] && return 0
      continue
    fi
    line=$(find_setup_line "$skip")
    if [ -n "$line" ]; then
      SETUP_URL=$(printf '%s' "$line" | sed -n 's/.* url=\([^ ]*\).*/\1/p' | tr -d '"')
      SETUP_TOKEN=$(printf '%s' "$line" | sed -n 's/.* token=\([^ ]*\).*/\1/p' | tr -d '"')
      return 0
    fi
  done
  systemctl is-active --quiet shed || {
    show_journal
    die "shed did not start. See the log above."
  }
}

# --- flows ----------------------------------------------------------------

upgrade() {
  local installed="" base
  [ ! -x "$BIN_PATH" ] || installed=$("$BIN_PATH" -version 2>/dev/null || true)
  if [ "$installed" = "$TAG" ]; then
    box "shed $TAG is already installed." "Config  $CONFIG_FILE"
    return 0
  fi
  step "Existing install found at $CONFIG_FILE: upgrading and keeping its config."
  download_release
  verify_release
  install_release
  systemctl daemon-reload
  base=$(restart_count)
  systemctl restart shed
  wait_for_shed "$base"
  box "shed upgraded to $("$BIN_PATH" -version)" \
    "Config    $CONFIG_FILE (unchanged)" \
    "Rollback  mv $BIN_PATH.prev $BIN_PATH && systemctl restart shed" \
    "Logs      journalctl -u shed -f"
}

fresh_install() {
  local skip=0 base
  collect_answers
  check_ports
  show_summary

  ensure_requirements
  download_release
  verify_release
  install_release
  write_config

  [ ! -f "$LOG_FILE" ] || skip=$(wc -l <"$LOG_FILE")
  systemctl daemon-reload
  base=$(restart_count)
  systemctl enable --now shed >/dev/null 2>&1 || {
    show_journal
    die "Could not start shed."
  }
  wait_for_shed "$base" "$skip"
  finish
}

finish() {
  local url=${SETUP_URL:-https://$SHED_DOMAIN/setup}
  local base=""
  [ -z "$SHED_BASE_DOMAIN" ] || base="  A/AAAA  *.$SHED_BASE_DOMAIN  ->  this server (generated app domains)"

  if [ -n "$SETUP_TOKEN" ]; then
    box "shed $("$BIN_PATH" -version) is running." \
      "Setup URL    $url" \
      "Setup token  $SETUP_TOKEN"
    say "Next steps"
    say "  1. Open the setup URL and enter the setup token."
  else
    box "shed $("$BIN_PATH" -version) is running."
    say "Next steps"
    say "  1. Open $url. If it asks for a setup token, find it with: journalctl -u shed | grep token"
  fi
  say "  2. Create the GitHub App when prompted, then install it on your repositories."
  say "  3. Sign in with an allowed GitHub account (${SHED_ALLOWED_USERS//,/, })."
  printf '\n'
  say "DNS"
  say "  A/AAAA  $SHED_DOMAIN  ->  this server (dashboard)"
  [ -z "$base" ] || say "$base"
  say "Firewall: allow inbound 80/tcp and 443/tcp."
  printf '\n'
  say "Config   $CONFIG_FILE"
  say "Logs     journalctl -u shed -f"
  say "Upgrade  re-run the install command, or use Settings -> Updates in the dashboard."
}

cleanup() {
  [ -z "$TMP" ] || rm -rf "$TMP"
}

main() {
  preflight
  TMP=$(mktemp -d)
  trap cleanup EXIT
  detect_mode
  [ "$INTERACTIVE" = 0 ] || bootstrap_gum
  header
  resolve_version
  if [ -f "$CONFIG_FILE" ]; then
    upgrade
  else
    fresh_install
  fi
}

main "$@"
