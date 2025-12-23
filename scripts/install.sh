#!/usr/bin/env sh

set -e

# error codes
# 1 general
# 2 insufficient perms

# Fetch latest release tag from GitHub API
get_latest_release() {
  if command -v curl > /dev/null 2>&1; then
    curl -sL "https://api.github.com/repos/warpdl/warpdl/releases/latest" |
      grep '"tag_name":' |
      sed -E 's/.*"([^"]+)".*/\1/'
  elif command -v wget > /dev/null 2>&1; then
    wget -qO- "https://api.github.com/repos/warpdl/warpdl/releases/latest" |
      grep '"tag_name":' |
      sed -E 's/.*"([^"]+)".*/\1/'
  else
    echo "v1.1.1"  # fallback version
  fi
}

# Allow override via WARPDL_VERSION env var, otherwise fetch latest
LATEST_RELEASE="${WARPDL_VERSION:-$(get_latest_release)}"

# Validate we got a version
if [ -z "$LATEST_RELEASE" ]; then
  echo "ERROR: Failed to determine latest release version" >&2
  exit 1
fi

# both os and arch are set to unknown by default
OS="unknown"
ARCH="unknown"
DL_FILENAME="warpdl_${LATEST_RELEASE#v}"
GITHUB_RELEASES_BASE_URL="https://github.com/warpdl/warpdl/releases/download/${LATEST_RELEASE}/"
# Cloudsmith download base URL for raw packages
# Format: https://dl.cloudsmith.io/public/OWNER/REPO/raw/names/NAME/versions/VERSION/FILENAME
CLOUDSMITH_BASE_URL="https://dl.cloudsmith.io/public/warpdl/warpdl/raw/names/warpdl/versions"
DOWNLOAD_SOURCE=""
DEBUG=0
INSTALL=1
CLEAN_EXIT=0
DISABLE_CURL=0
CUSTOM_INSTALL_PATH=""
BINARY_INSTALLED_PATH=""
NO_REPO=0
DISTRO_ID=""
DISTRO_ID_LIKE=""
DISTRO_VERSION=""
SUDO=""

tempdir=""
filename=""

cleanup() {
  exit_code=$?
  if [ "$exit_code" -ne 0 ] && [ "$CLEAN_EXIT" -ne 1 ]; then
    log "ERROR: script failed during execution"

    if [ "$DEBUG" -eq 0 ]; then
      log "For more verbose output, re-run this script with the debug flag (./install.sh --debug)"
    fi
  fi

  if [ -n "$tempdir" ]; then
    delete_tempdir
    echo ""
  fi

  clean_exit "$exit_code"
}
trap cleanup EXIT
trap cleanup INT

clean_exit() {
  CLEAN_EXIT=1
  exit "$1"
}

log() {
  # print to stderr
  >&2 echo "$1"
}

log_debug() {
  if [ "$DEBUG" -eq 1 ]; then
    # print to stderr
    >&2 echo "DEBUG: $1"
  fi
}

log_warning() {
  # print to stderr
  >&2 echo "WARNING: $1"
}

delete_tempdir() {
  log_debug "Removing temp directory"
  rm -rf "$tempdir"
  tempdir=""
}

# Detect Linux distribution by parsing /etc/os-release
# Sets DISTRO_ID, DISTRO_ID_LIKE, and DISTRO_VERSION globals
detect_distro() {
  if [ ! -f /etc/os-release ]; then
    log_debug "No /etc/os-release found, cannot detect distro"
    return 1
  fi

  # Parse /etc/os-release - extract ID, ID_LIKE, and VERSION_ID
  # Using sed to handle the format: KEY="value" or KEY=value
  DISTRO_ID=$(sed -n 's/^ID="\?\([^"]*\)"\?$/\1/p' /etc/os-release | head -1)
  DISTRO_ID_LIKE=$(sed -n 's/^ID_LIKE="\?\([^"]*\)"\?$/\1/p' /etc/os-release | head -1)
  DISTRO_VERSION=$(sed -n 's/^VERSION_ID="\?\([^"]*\)"\?$/\1/p' /etc/os-release | head -1)

  log_debug "Detected distro: ID=$DISTRO_ID, ID_LIKE=$DISTRO_ID_LIKE, VERSION=$DISTRO_VERSION"
  return 0
}

# Detect if running inside a container (Docker, Podman, LXC)
# Returns 0 if in container, 1 otherwise
is_container() {
  # Check for Docker
  if [ -f /.dockerenv ]; then
    log_debug "Detected Docker container (/.dockerenv exists)"
    return 0
  fi

  # Check for Podman
  if [ -f /run/.containerenv ]; then
    log_debug "Detected Podman container (/run/.containerenv exists)"
    return 0
  fi

  # Check cgroup for container indicators (docker, lxc, kubepods)
  if [ -f /proc/1/cgroup ]; then
    if grep -qE '(docker|lxc|kubepods|containerd)' /proc/1/cgroup 2>/dev/null; then
      log_debug "Detected container via /proc/1/cgroup"
      return 0
    fi
  fi

  log_debug "Not running in a container"
  return 1
}

# Check if systemd is running as init system
# Returns 0 if systemd is running, 1 otherwise
has_systemd() {
  if [ -d /run/systemd/system ]; then
    log_debug "systemd detected (/run/systemd/system exists)"
    return 0
  fi

  log_debug "systemd not detected"
  return 1
}

# Check if OpenRC is available
# Returns 0 if OpenRC is available, 1 otherwise
has_openrc() {
  if command -v rc-service > /dev/null 2>&1; then
    log_debug "OpenRC detected (rc-service command available)"
    return 0
  fi

  log_debug "OpenRC not detected"
  return 1
}

# Determine if native package manager should be used
# Returns 0 if native pkg should be used, 1 otherwise
# Native packages are preferred when:
# - NO_REPO flag is not set (user hasn't disabled it)
# - Not in a container OR has a proper init system (systemd/openrc)
should_use_native_pkg() {
  # User explicitly disabled repo-based installation
  if [ "$NO_REPO" -eq 1 ]; then
    log_debug "Native package disabled by NO_REPO flag"
    return 1
  fi

  # If not in a container, use native package
  if ! is_container; then
    log_debug "Not in container, native package recommended"
    return 0
  fi

  # In a container - check for init system
  # Containers with systemd or openrc can still use native packages
  if has_systemd; then
    log_debug "Container with systemd, native package recommended"
    return 0
  fi

  if has_openrc; then
    log_debug "Container with OpenRC, native package recommended"
    return 0
  fi

  # Container without proper init system - use binary install
  log_debug "Container without init system, native package not recommended"
  return 1
}

# Ensure sudo access is available
# Sets SUDO="" if root, SUDO="sudo" otherwise
# Returns 1 if sudo is required but not available
ensure_sudo() {
  if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
    log_debug "Running as root, no sudo needed"
    return 0
  fi

  if ! command -v sudo > /dev/null 2>&1; then
    log_warning "sudo command not found and not running as root"
    return 1
  fi

  # Test sudo access
  if ! sudo -v > /dev/null 2>&1; then
    log_warning "Unable to obtain sudo access"
    return 1
  fi

  SUDO="sudo"
  log_debug "Using sudo for privileged operations"
  return 0
}

# Clean up existing binary installations not managed by package managers
# Stops running daemon and removes socket file
cleanup_existing_binary() {
  log_debug "Checking for existing warpdl installations..."

  # Stop any running daemon first
  if command -v warpdl > /dev/null 2>&1; then
    log_debug "Stopping warpdl daemon if running..."
    warpdl stop-daemon 2>/dev/null || true
  fi

  # Remove daemon socket if it exists
  if [ -e "/tmp/warpdl.sock" ]; then
    log_debug "Removing daemon socket /tmp/warpdl.sock"
    $SUDO rm -f "/tmp/warpdl.sock" 2>/dev/null || true
  fi

  # Check /usr/local/bin/warpdl
  if [ -f "/usr/local/bin/warpdl" ]; then
    is_managed=0
    # Check if managed by dpkg
    if command -v dpkg > /dev/null 2>&1; then
      if dpkg -S "/usr/local/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/local/bin/warpdl is managed by dpkg"
      fi
    fi
    # Check if managed by rpm
    if [ "$is_managed" -eq 0 ] && command -v rpm > /dev/null 2>&1; then
      if rpm -qf "/usr/local/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/local/bin/warpdl is managed by rpm"
      fi
    fi
    # Check if managed by apk
    if [ "$is_managed" -eq 0 ] && command -v apk > /dev/null 2>&1; then
      if apk info --who-owns "/usr/local/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/local/bin/warpdl is managed by apk"
      fi
    fi
    # Remove if not managed by any package manager
    if [ "$is_managed" -eq 0 ]; then
      log_debug "Removing unmanaged binary /usr/local/bin/warpdl"
      $SUDO rm -f "/usr/local/bin/warpdl"
    fi
  fi

  # Check /usr/bin/warpdl
  if [ -f "/usr/bin/warpdl" ]; then
    is_managed=0
    # Check if managed by dpkg
    if command -v dpkg > /dev/null 2>&1; then
      if dpkg -S "/usr/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/bin/warpdl is managed by dpkg"
      fi
    fi
    # Check if managed by rpm
    if [ "$is_managed" -eq 0 ] && command -v rpm > /dev/null 2>&1; then
      if rpm -qf "/usr/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/bin/warpdl is managed by rpm"
      fi
    fi
    # Check if managed by apk
    if [ "$is_managed" -eq 0 ] && command -v apk > /dev/null 2>&1; then
      if apk info --who-owns "/usr/bin/warpdl" > /dev/null 2>&1; then
        is_managed=1
        log_debug "/usr/bin/warpdl is managed by apk"
      fi
    fi
    # Remove if not managed by any package manager
    if [ "$is_managed" -eq 0 ]; then
      log_debug "Removing unmanaged binary /usr/bin/warpdl"
      $SUDO rm -f "/usr/bin/warpdl"
    fi
  fi
}

# Set up Debian/Ubuntu APT repository and install warpdl
# Returns 1 on failure
setup_deb_repo() {
  log "Setting up Cloudsmith APT repository..."

  if ! curl -1sLf "https://dl.cloudsmith.io/public/warpdl/warpdl/setup.deb.sh" | $SUDO bash; then
    log_warning "Failed to set up Cloudsmith APT repository"
    return 1
  fi

  log "Updating package lists..."
  if ! $SUDO apt-get update; then
    log_warning "Failed to update package lists"
    return 1
  fi

  log "Installing warpdl..."
  if ! $SUDO apt-get install -y warpdl; then
    log_warning "Failed to install warpdl via apt-get"
    return 1
  fi

  return 0
}

# Set up RPM repository (Fedora/RHEL/CentOS) and install warpdl
# Returns 1 on failure
setup_rpm_repo() {
  log "Setting up Cloudsmith RPM repository..."

  if ! curl -1sLf "https://dl.cloudsmith.io/public/warpdl/warpdl/setup.rpm.sh" | $SUDO bash; then
    log_warning "Failed to set up Cloudsmith RPM repository"
    return 1
  fi

  log "Installing warpdl..."
  # Prefer dnf over yum
  if command -v dnf > /dev/null 2>&1; then
    if ! $SUDO dnf install -y warpdl; then
      log_warning "Failed to install warpdl via dnf"
      return 1
    fi
  elif command -v yum > /dev/null 2>&1; then
    if ! $SUDO yum install -y warpdl; then
      log_warning "Failed to install warpdl via yum"
      return 1
    fi
  else
    log_warning "Neither dnf nor yum found"
    return 1
  fi

  return 0
}

# Set up Alpine APK repository and install warpdl
# Returns 1 on failure
setup_alpine_repo() {
  log "Setting up Cloudsmith Alpine repository..."

  if ! curl -1sLf "https://dl.cloudsmith.io/public/warpdl/warpdl/setup.alpine.sh" | $SUDO -E bash; then
    log_warning "Failed to set up Cloudsmith Alpine repository"
    return 1
  fi

  log "Installing warpdl..."
  if ! $SUDO apk add warpdl; then
    log_warning "Failed to install warpdl via apk"
    return 1
  fi

  return 0
}

# Suggest package manager alternatives for platforms without native package support
suggest_package_manager() {
  case "$uname_os" in
    Darwin)
      log "TIP: On macOS, you can install warpdl via Homebrew:"
      log "  brew install warpdl/tap/warpdl"
      ;;
    *MINGW*|*MSYS*)
      log "TIP: On Windows, you can install warpdl via Scoop:"
      log "  scoop bucket add warpdl https://github.com/warpdl/scoop-bucket"
      log "  scoop install warpdl"
      ;;
  esac
  log "Proceeding with direct binary install..."
}

linux_shell() {
  user="$(whoami)"
  grep -E "^$user:" < /etc/passwd | cut -f 7 -d ":" | head -1
}

macos_shell() {
  dscl . -read ~/ UserShell | sed 's/UserShell: //'
}

# we currently only support Git Bash for Windows with this script
# so the shell will always be /usr/bin/bash
windows_shell() {
  echo "/usr/bin/bash"
}

# Download with Cloudsmith primary, GitHub fallback
# Returns HTTP status code
download_with_fallback() {
  local_output_file="$1"
  local_component="$2"

  # Strip 'v' prefix from version for Cloudsmith URL
  version_no_v="${LATEST_RELEASE#v}"

  # Construct URLs
  # Cloudsmith: /names/warpdl/versions/VERSION/FILENAME
  cloudsmith_url="${CLOUDSMITH_BASE_URL}/${version_no_v}/${DL_FILENAME}"
  github_url="${GITHUB_RELEASES_BASE_URL}${DL_FILENAME}"

  log_debug "Trying Cloudsmith: $cloudsmith_url"

  if [ "$curl_installed" -eq 0 ]; then
    # Try Cloudsmith first with curl
    set +e
    cs_headers=$(curl --tlsv1.2 --proto "=https" -w "%{http_code}" --silent --retry 2 --connect-timeout 10 -o "$local_output_file" -LN -D - "$cloudsmith_url" 2>&1)
    cs_status="$(echo "$cs_headers" | tail -1)"
    set -e

    if [ "$cs_status" = "200" ]; then
      DOWNLOAD_SOURCE="Cloudsmith"
      log_debug "Downloaded from Cloudsmith"
      echo "200"
      return
    fi

    log_debug "Cloudsmith failed (status: $cs_status), falling back to GitHub Releases"

    # Fallback to GitHub
    status_code=$(curl_download "$github_url" "$local_output_file" "$local_component")
    if [ "$status_code" = "200" ]; then
      DOWNLOAD_SOURCE="GitHub Releases"
    fi
    echo "$status_code"

  elif [ "$wget_installed" -eq 0 ]; then
    # Try Cloudsmith first with wget
    set +e
    wget --secure-protocol=TLSv1_2 --https-only -q -t 2 --connect-timeout=10 -O "$local_output_file" "$cloudsmith_url" 2>/dev/null
    wget_exit=$?
    set -e

    if [ "$wget_exit" -eq 0 ] && [ -f "$local_output_file" ] && [ -s "$local_output_file" ]; then
      DOWNLOAD_SOURCE="Cloudsmith"
      log_debug "Downloaded from Cloudsmith"
      echo "200"
      return
    fi

    log_debug "Cloudsmith failed, falling back to GitHub Releases"

    # Fallback to GitHub
    status_code=$(wget_download "$github_url" "$local_output_file" "$local_component")
    if [ "$status_code" = "200" ]; then
      DOWNLOAD_SOURCE="GitHub Releases"
    fi
    echo "$status_code"
  fi
}

# exit code
# 0=installed
# 1=path not writable
# 2=path not in PATH
# 3=path not a directory
# 4=path not found
install_binary() {
  install_dir="$1"
  # defaults to true
  require_dir_in_path="$2"
  # defaults to false
  create_if_not_exist="$3"

  if [ "$require_dir_in_path" != "false" ] && ! is_dir_in_path "$install_dir"; then
    return 2
  fi

  if [ "$create_if_not_exist" = "true" ] && [ ! -e "$install_dir" ]; then
    log_debug "$install_dir is in PATH but doesn't exist"
    log_debug "Creating $install_dir"
    mkdir -m 755 "$install_dir" > /dev/null 2>&1
  fi

  if [ ! -e "$install_dir" ]; then
    return 4
  fi

  if [ ! -d "$install_dir" ]; then
    return 3
  fi

  if ! is_path_writable "$install_dir"; then
    return 1
  fi

  log_debug "Moving binary to $install_dir"
  mv -f "$extract_dir/warpdl" "$install_dir"
  return 0
}

curl_download() {
  url="$1"
  output_file="$2"
  component="$3"

  # allow curl to fail w/o exiting
  set +e
  headers=$(curl --tlsv1.2 --proto "=https" -w "%{http_code}" --silent --retry 5 -o "$output_file" -LN -D - "$url" 2>&1)
  exit_code=$?
  set -e

  status_code="$(echo "$headers" | tail -1)"

  if [ "$status_code" -ne 200 ]; then
    log_debug "Request failed with http status $status_code"
    log_debug "Response headers:"
    log_debug "$headers"
  fi

  if [ "$exit_code" -ne 0 ]; then
    log "ERROR: curl failed with exit code $exit_code"

    if [ "$exit_code" -eq 60 ]; then
      log ""
      log "Ensure the ca-certificates package is installed for your distribution"
    fi
    clean_exit 1
  fi

  if [ "$status_code" -eq 200 ]; then
    if [ "$component" = "Binary" ]; then
      parse_version_header
    fi
  fi

  # this could be >255, so print HTTP status code rather than using as return code
  echo "$status_code"
}

# note: wget does not retry on 5xx
wget_download() {
  url="$1"
  output_file="$2"
  component="$3"

  security_flags="--secure-protocol=TLSv1_2 --https-only"
  # determine if using BusyBox wget (bad) or GNU wget (good)
  (wget --help 2>&1 | head -1 | grep -iv busybox > /dev/null 2>&1) || security_flags=""
  # only print this warning once per script invocation
  if [ -z "$security_flags" ] && [ "$component" = "Binary" ]; then
    log_debug "Skipping additional security flags that are unsupported by BusyBox wget"
    # log to stderr b/c this function's stdout is parsed
    log_warning "This system's wget binary is provided by BusyBox. Warpdl strongly suggests installing GNU wget, which provides additional security features."
  fi

  # allow wget to fail w/o exiting
  set +e
  # we explicitly disable shellcheck here b/c security_flags isn't parsed properly when quoted
  # shellcheck disable=SC2086
  headers=$(wget $security_flags -q -t 5 -S -O "$output_file" "$url" 2>&1)
  exit_code=$?
  set -e

  status_code="$(echo "$headers" | grep -o -E '^\s*HTTP/[0-9.]+ [0-9]{3}' | tail -1 | grep -o -E '[0-9]{3}')"
  # it's possible for this value to be blank, so confirm that it's a valid status code
  valid_status_code=0
  if expr "$status_code" : '[0-9][0-9][0-9]$'>/dev/null; then
    valid_status_code=1
  fi

  if [ "$exit_code" -ne 0 ]; then
    if [ "$valid_status_code" -eq 1 ]; then
      # print the code and continue
      log_debug "Request failed with http status $status_code"
      log_debug "Response headers:"
      log_debug "$headers"
    else
      # exit immediately
      log "ERROR: wget failed with exit code $exit_code"

      if [ "$exit_code" -eq 5 ]; then
        log ""
        log "Ensure the ca-certificates package is installed for your distribution"
      fi
      clean_exit 1
    fi
  fi

  if [ "$status_code" -eq 200 ]; then
    if [ "$component" = "Binary" ]; then
      parse_version_header
    fi
  fi

  # this could be >255, so print HTTP status code rather than using as return code
  echo "$status_code"
}

parse_version_header() {
  if [ -n "$latest_version" ]; then
    log_debug "Downloaded CLI $latest_version"
  fi
}

check_http_status() {
  status_code="$1"
  component="$2"

  if [ "$status_code" -ne 200 ]; then
    error="ERROR: $component download failed with status code $status_code."
    if [ "$status_code" -ne 404 ]; then
      error="${error} Please try again."
    fi

    log ""
    log "$error"

    if [ "$status_code" -eq 404 ]; then
      log ""
      log "Please report this issue:"
      log "https://github.com/warpdl/warpdl/issues/new?template=bug_report.md&title=[BUG]%20Unexpected%20404%20using%20CLI%20install%20script"
    fi

    clean_exit 1
  fi
}

is_dir_in_path() {
  dir="$1"
  # ensure dir is the full path and not a substring of some longer path.
  # after performing a regex search, perform another search w/o regex to filter out matches due to special characters in `$dir`
  echo "$PATH" | grep -o -E "(^|:)$dir(:|$)" | grep -q -F "$dir"
}

is_path_writable() {
  dir="$1"
  test -w "$dir"
}

# Binary download and install logic
do_binary_install() {
  set +e
  curl_binary="$(command -v curl)"
  wget_binary="$(command -v wget)"

  # check if curl is available
  [ "$DISABLE_CURL" -eq 0 ] && [ -x "$curl_binary" ]
  curl_installed=$? # 0 = yes

  # check if wget is available
  [ -x "$wget_binary" ]
  wget_installed=$? # 0 = yes
  set -e

  if [ "$curl_installed" -eq 0 ] || [ "$wget_installed" -eq 0 ]; then
    # create hidden temp dir in user's home directory to ensure no other users have write perms
    tempdir="$(mktemp -d ~/.tmp.XXXXXXXX)"
    log_debug "Using temp directory $tempdir"

    log "Downloading Warpdl CLI"
    file="${DL_FILENAME}"
    filename="$tempdir/$file"

    if [ "$curl_installed" -eq 0 ]; then
      log_debug "Using $curl_binary for requests"
      log_debug "Downloading binary..."
      status_code=$(download_with_fallback "$filename" "Binary")
      check_http_status "$status_code" "Binary"

    elif [ "$wget_installed" -eq 0 ]; then
      log_debug "Using $wget_binary for requests"
      log_debug "Downloading binary..."
      status_code=$(download_with_fallback "$filename" "Binary")
      check_http_status "$status_code" "Binary"
    fi
  else
    log "ERROR: You must have curl or wget installed"
    clean_exit 1
  fi

  if [ "$format" = "tar.gz" ] || [ "$format" = "zip" ]; then
    if [ "$format" = "tar.gz" ]; then
      filename="${tempdir}/${DL_FILENAME}"

      # extract
      extract_dir="$tempdir/x"
      mkdir "$extract_dir"
      log_debug "Extracting tarball to $extract_dir"
      tar -xzf "$filename" -C "$extract_dir"

      # set appropriate perms
      chown "$(id -u):$(id -g)" "$extract_dir/warpdl"
      chmod 755 "$extract_dir/warpdl"
    elif [ "$format" = "zip" ]; then
      mv -f "$filename" "$filename.zip"
      filename="$filename.zip"

      # extract
      extract_dir="$tempdir/x"
      mkdir "$extract_dir"
      log_debug "Extracting zip to $extract_dir"
      unzip -d "$extract_dir" "$filename"

      # set appropriate perms
      chown "$(id -u):$(id -g)" "$extract_dir/warpdl"
      chmod 755 "$extract_dir/warpdl"
    fi

    # install
    if [ "$INSTALL" -eq 1 ]; then
      log "Installing..."
      binary_installed=0
      found_non_writable_path=0

      if [ "$CUSTOM_INSTALL_PATH" != "" ]; then
        # install to this directory or fail; don't try any other paths

        # capture exit code without exiting
        set +e
        install_binary "$CUSTOM_INSTALL_PATH" "false" "false"
        exit_code=$?
        set -e
        if [ $exit_code -eq 0 ]; then
          binary_installed=1
          BINARY_INSTALLED_PATH="$CUSTOM_INSTALL_PATH"
        elif [ $exit_code -eq 1 ]; then
          log "Install path is not writable: \"$CUSTOM_INSTALL_PATH\""
          clean_exit 2
        elif [ $exit_code -eq 4 ]; then
          log "Install path does not exist: \"$CUSTOM_INSTALL_PATH\""
          clean_exit 1
        else
          log "Install path is not a valid directory: \"$CUSTOM_INSTALL_PATH\""
          clean_exit 1
        fi
      fi

      # check for an existing warpdl binary
      if [ "$binary_installed" -eq 0 ]; then
        existing_install_dir="$(command -v warpdl || true)"
        if [ "$existing_install_dir" != "" ]; then
          install_dir="$(dirname "$existing_install_dir")"
          # capture exit code without exiting
          set +e
          install_binary "$install_dir"
          exit_code=$?
          set -e
          if [ $exit_code -eq 0 ]; then
            binary_installed=1
            BINARY_INSTALLED_PATH="$install_dir"
          elif [ $exit_code -eq 1 ]; then
            found_non_writable_path=1
          fi
        fi
      fi

      if [ "$binary_installed" -eq 0 ]; then
        install_dir="/usr/local/bin"
        # capture exit code without exiting
        set +e
        install_binary "$install_dir"
        exit_code=$?
        set -e
        if [ $exit_code -eq 0 ]; then
          binary_installed=1
          BINARY_INSTALLED_PATH="$install_dir"
        elif [ $exit_code -eq 1 ]; then
          found_non_writable_path=1
        fi
      fi

      if [ "$binary_installed" -eq 0 ]; then
        install_dir="/usr/bin"
        # capture exit code without exiting
        set +e
        install_binary "$install_dir"
        exit_code=$?
        set -e
        if [ $exit_code -eq 0 ]; then
          binary_installed=1
          BINARY_INSTALLED_PATH="$install_dir"
        elif [ $exit_code -eq 1 ]; then
          found_non_writable_path=1
        fi
      fi

      if [ "$binary_installed" -eq 0 ]; then
        install_dir="/usr/sbin"
        # capture exit code without exiting
        set +e
        install_binary "$install_dir"
        exit_code=$?
        set -e
        if [ $exit_code -eq 0 ]; then
          binary_installed=1
          BINARY_INSTALLED_PATH="$install_dir"
        elif [ $exit_code -eq 1 ]; then
          found_non_writable_path=1
        fi
      fi

      if [ "$binary_installed" -eq 0 ]; then
        # run again for this directory, but this time create it if it doesn't exist
        # this fixes an issue with clean installs on macOS 12+
        install_dir="/usr/local/bin"
        # capture exit code without exiting
        set +e
        install_binary "$install_dir" "true" "true"
        exit_code=$?
        set -e
        if [ $exit_code -eq 0 ]; then
          binary_installed=1
          BINARY_INSTALLED_PATH="$install_dir"
        elif [ $exit_code -eq 1 ]; then
          found_non_writable_path=1
        fi
      fi

      if [ "$binary_installed" -eq 0 ]; then
        if [ "$found_non_writable_path" -eq 1 ]; then
          log "Unable to write to bin directory; please re-run with \`sudo\` or adjust your PATH"
          clean_exit 2
        else
          log "No supported bin directories are available; please adjust your PATH"
          clean_exit 1
        fi
      fi
    else
      log_debug "Moving binary to $(pwd) (cwd)"
      mv -f "$extract_dir/warpdl" .
    fi

    delete_tempdir

    if [ "$INSTALL" -eq 1 ]; then
      message="Installed Warpdl CLI $("$BINARY_INSTALLED_PATH"/warpdl -v)"
      if [ "$CUSTOM_INSTALL_PATH" != "" ]; then
        message="$message to $BINARY_INSTALLED_PATH"
      fi
      if [ -n "$DOWNLOAD_SOURCE" ]; then
        message="$message (from $DOWNLOAD_SOURCE)"
      fi
      echo "$message"
    else
      echo "Warpdl CLI saved to ./warpdl"
    fi
  fi
}

find_install_path_arg=0
# flag parsing
for arg; do
  if [ "$find_install_path_arg" -eq 1 ]; then
    CUSTOM_INSTALL_PATH="$arg"
    find_install_path_arg=0
    continue
  fi

  case "$arg" in
    --debug)
      DEBUG=1
      ;;
    --no-install)
      INSTALL=0
      ;;
    --disable-curl)
      DISABLE_CURL=1
      ;;
    --install-path)
      find_install_path_arg=1
      ;;
    --no-repo)
      NO_REPO=1
      ;;
  esac
done

if [ "$find_install_path_arg" -eq 1 ]; then
  log "You must provide a path when specifying --install-path"
  clean_exit 1
fi

# identify OS
uname_os=$(uname -s)
case "$uname_os" in
  Darwin)    OS="macos"   ;;
  Linux)     OS="linux"   ;;
  FreeBSD)   OS="freebsd" ;;
  OpenBSD)   OS="openbsd" ;;
  NetBSD)    OS="netbsd"  ;;
  *MINGW64*) OS="windows" ;;
  *MINGW*|*MSYS*)
             OS="windows" ;;
  *)
    log "ERROR: Unsupported OS '$uname_os'"
    log ""
    log "Please report this issue:"
    log "https://github.com/warpdl/warpdl/issues/new?template=bug_report.md&title=[BUG]%20Unsupported%20OS"
    clean_exit 1
    ;;
esac

log_debug "Detected OS '$OS'"

# identify arch
uname_machine=$(uname -m)
if [ "$uname_machine" = "i386" ] || [ "$uname_machine" = "i686" ]; then
  ARCH="i386"
elif [ "$uname_machine" = "amd64" ] || [ "$uname_machine" = "x86_64" ]; then
  ARCH="amd64"
elif [ "$uname_machine" = "armv6" ] || [ "$uname_machine" = "armv6l" ]; then
  ARCH="armv6"
elif [ "$uname_machine" = "armv7" ] || [ "$uname_machine" = "armv7l" ]; then
  ARCH="armv7"
# armv8?
elif [ "$uname_machine" = "arm64" ] || [ "$uname_machine" = "aarch64" ]; then
  ARCH="arm64"
else
  log "ERROR: Unsupported architecture '$uname_machine'"
  log ""
  log "Please report this issue:"
  log "https://github.com/warpdl/warpdl/issues/new?template=bug_report.md&title=[BUG]%20Unsupported%20architecture"
  clean_exit 1
fi

log_debug "Detected architecture '$ARCH'"

# identify format
if [ "$OS" = "windows" ]; then
  format="zip"
else
  format="tar.gz"
fi

log_debug "Detected format '$format'"

DL_FILENAME="${DL_FILENAME}_${OS}_${ARCH}.$format"
url="${GITHUB_RELEASES_BASE_URL}${DL_FILENAME}"

# Main execution flow - handle OS-specific installation strategies
case "$OS" in
  linux)
    detect_distro

    native_pkg_success=0
    if should_use_native_pkg && ensure_sudo; then
      cleanup_existing_binary

      # Try native package install based on distro
      set +e
      case "$DISTRO_ID" in
        ubuntu|debian|linuxmint|pop|elementary|zorin|raspbian)
          log_debug "Attempting APT package install for $DISTRO_ID"
          if setup_deb_repo; then
            native_pkg_success=1
          fi
          ;;
        fedora|rhel|centos|rocky|almalinux|ol)
          log_debug "Attempting RPM package install for $DISTRO_ID"
          if setup_rpm_repo; then
            native_pkg_success=1
          fi
          ;;
        alpine)
          log_debug "Attempting Alpine package install"
          if setup_alpine_repo; then
            native_pkg_success=1
          fi
          ;;
        *)
          # Check ID_LIKE for derivative distros
          case "$DISTRO_ID_LIKE" in
            *debian*|*ubuntu*)
              log_debug "Attempting APT package install for $DISTRO_ID (like debian/ubuntu)"
              if setup_deb_repo; then
                native_pkg_success=1
              fi
              ;;
            *fedora*|*rhel*)
              log_debug "Attempting RPM package install for $DISTRO_ID (like fedora/rhel)"
              if setup_rpm_repo; then
                native_pkg_success=1
              fi
              ;;
          esac
          ;;
      esac
      set -e

      if [ "$native_pkg_success" -eq 1 ]; then
        log "Installed Warpdl CLI via native package manager"
        clean_exit 0
      else
        log_debug "Native package install failed, falling back to binary install"
      fi
    fi

    # Fall through to binary install
    do_binary_install
    ;;

  macos)
    suggest_package_manager
    do_binary_install
    ;;

  windows)
    suggest_package_manager
    do_binary_install
    ;;

  *)
    # FreeBSD, OpenBSD, NetBSD, etc.
    do_binary_install
    ;;
esac
