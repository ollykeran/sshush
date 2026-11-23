#!/usr/bin/env bash
# Builds sshush's MSIX package. Called by `just pkg msix` and `just msix-dev`.
#
#   scripts/msix.sh version  <version>
#   scripts/msix.sh stage    <version> <arch> <bin_dir> <out_dir>
#   scripts/msix.sh pack     <stage_dir> <msix_file>
#   scripts/msix.sh register <stage_dir>
#   scripts/msix.sh tools
#   scripts/msix.sh sign     <msix_file>
#   scripts/msix.sh trust
#   scripts/msix.sh install  <msix_file>
#   scripts/msix.sh certify  <msix_file> <report_xml>
#
# version prints the MSIX version a release version becomes (see msix_version).
# stage fills out_dir with the binaries, the logos and an AppxManifest.xml.
# pack turns it into an .msix with makeappx. register installs the staged
# directory as it is, unsigned, for trying the package on this machine: that
# needs Developer Mode.
#
# The rest try the packed .msix the way the Store will install it. tools
# fetches makeappx and signtool (no Windows SDK needed). sign signs the
# package with a local self-signed certificate for its Publisher, trust lets
# this machine install packages signed by it (it asks for an administrator),
# and install installs it. certify runs the Windows App Certification Kit on
# the package, as the Store does after a submission. Everything but stage and pack on Linux runs on
# Windows (Git Bash) or under WSL.
#
# The package identity comes from the environment, so a Store build can use
# the values Partner Center reserved for the app while a local one needs none:
#   MSIX_IDENTITY_NAME          Package/Identity/Name           (default sshush.dev)
#   MSIX_PUBLISHER              Package/Identity/Publisher      (default CN=sshush-dev)
#   MSIX_PUBLISHER_DISPLAY_NAME Package/Properties/PublisherDisplayName (default sshush)
#   MSIX_DISPLAY_NAME           the app's name, which must be one reserved
#                               for it in Partner Center   (default sshush)
set -euo pipefail
# Bash 5.2 reads & in a ${var//pattern/replacement} replacement as the matched
# text, which would mangle the &amp; that xml_escape writes.
shopt -u patsub_replacement 2>/dev/null || true

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template="$here/packaging/msix/AppxManifest.xml.in"
assets="$here/packaging/msix/Assets"

die() { echo "msix: $*" >&2; exit 1; }

# sdk_tool runs a Windows SDK program. Git Bash and MSYS2 rewrite arguments
# that look like Unix paths, which turns makeappx's /o into O:/; the paths
# given here are already Windows paths (windows_path), so that is turned off.
sdk_tool() {
  MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' "$@"
}

# msix_version turns a release version into the four-part one MSIX requires.
# The Store requires the fourth part to be 0, and every package uploaded for
# the app to have its own version, so a pre-release cannot share its release's
# number. The patch number is multiplied by 100 to make room for them:
#
#   1.2.3-rc.1  -> 1.2.301.0   (the last number in the suffix, 0 to 98)
#   1.2.3-rc.2  -> 1.2.302.0
#   1.2.3       -> 1.2.399.0
#
# so a pre-release sorts after the previous release and before its own, and a
# Store flight's testers move onto the release when it is published. Anything
# that is not a release version, such as "dev", becomes 0.0.1.0.
msix_version() {
  local v="${1#v}"
  v="${v%%+*}"
  local base="${v%%-*}" pre=""
  [[ "$v" == *-* ]] && pre="${v#*-}"
  if ! [[ "$base" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "0.0.1.0"
    return
  fi
  local major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}" n=99
  if [ -n "$pre" ]; then
    n=0
    if [[ "$pre" =~ ([0-9]+)[^0-9]*$ ]]; then n=$((10#${BASH_REMATCH[1]})); fi
    [ "$n" -le 98 ] || die "$1: a pre-release number above 98 does not fit the MSIX version"
  fi
  local build=$((10#$patch * 100 + n))
  # Each part of an MSIX version is 16 bits.
  if [ $((10#$major)) -gt 65535 ] || [ $((10#$minor)) -gt 65535 ] || [ "$build" -gt 65535 ]; then
    die "$1 does not fit an MSIX version (each part at most 65535, the patch at most 654)"
  fi
  echo "$((10#$major)).$((10#$minor)).$build.0"
}

# xml_escape makes a value safe to put in an attribute or element.
xml_escape() {
  local s="$1"
  s="${s//&/&amp;}"; s="${s//</&lt;}"; s="${s//>/&gt;}"; s="${s//\"/&quot;}"
  printf '%s' "$s"
}

stage() {
  local version="$1" arch="$2" bin_dir="$3" out_dir="$4"
  local exe
  for exe in sshush.exe sshushd.exe sshush-tray.exe; do
    [ -f "$bin_dir/$exe" ] || die "$bin_dir/$exe not found; run just build windows first"
  done
  rm -rf "$out_dir"
  mkdir -p "$out_dir/Assets"
  cp "$bin_dir/sshush.exe" "$bin_dir/sshushd.exe" "$bin_dir/sshush-tray.exe" "$out_dir/"
  cp "$assets"/*.png "$out_dir/Assets/"

  local name publisher display app_name
  name="$(xml_escape "${MSIX_IDENTITY_NAME:-sshush.dev}")"
  publisher="$(xml_escape "${MSIX_PUBLISHER:-CN=sshush-dev}")"
  display="$(xml_escape "${MSIX_PUBLISHER_DISPLAY_NAME:-sshush}")"
  app_name="$(xml_escape "${MSIX_DISPLAY_NAME:-sshush}")"
  # Bash substitution rather than sed: a publisher such as
  # "CN=Jane, O=Example, C=GB" holds characters sed would need escaped.
  local msixv manifest
  msixv="$(msix_version "$version")"
  manifest="$(<"$template")"
  manifest="${manifest//@IDENTITY_NAME@/$name}"
  manifest="${manifest//@PUBLISHER@/$publisher}"
  manifest="${manifest//@PUBLISHER_DISPLAY_NAME@/$display}"
  manifest="${manifest//@DISPLAY_NAME@/$app_name}"
  manifest="${manifest//@VERSION@/$msixv}"
  manifest="${manifest//@ARCH@/$arch}"
  if grep -qE '@[A-Z_]+@' <<<"$manifest"; then
    die "unfilled placeholder in the manifest: $(grep -oE '@[A-Z_]+@' <<<"$manifest" | head -1)"
  fi
  printf '%s\n' "$manifest" >"$out_dir/AppxManifest.xml"
  echo "Staged $msixv ($arch) in $out_dir"
}

# windows_path converts a path for a Windows program: wslpath under WSL,
# cygpath under Git Bash or MSYS2, and the path itself on Windows proper.
windows_path() {
  if command -v wslpath >/dev/null 2>&1; then
    wslpath -w "$1"
  elif command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$1"
  else
    printf '%s' "$1"
  fi
}

# ps runs a PowerShell command and prints its output without the \r.
ps() {
  command -v powershell.exe >/dev/null 2>&1 || die "powershell.exe not found: this step runs on Windows or under WSL"
  powershell.exe -NoProfile -Command "\$ErrorActionPreference = 'Stop'; $1" | tr -d '\r'
}

# ps_quote makes a value safe inside a single-quoted PowerShell string.
ps_quote() { printf '%s' "${1//\'/\'\'}"; }

# windows_dir prints a directory under %LOCALAPPDATA% as this shell sees it.
# Windows programs do not reliably accept paths into the WSL file system, so
# what they read or write goes there.
windows_dir() {
  local base
  base="$(ps '[Environment]::GetFolderPath("LocalApplicationData")')"
  [ -n "$base" ] || die "could not find %LOCALAPPDATA%"
  if command -v wslpath >/dev/null 2>&1; then
    base="$(wslpath -u "$base")"
  elif command -v cygpath >/dev/null 2>&1; then
    base="$(cygpath -u "$base")"
  fi
  printf '%s/%s' "$base" "$1"
}

# The version of the SDK build tools that tools fetches: makeappx and signtool
# without installing the Windows SDK, from Microsoft's NuGet package.
buildtools_version="${MSIX_BUILDTOOLS_VERSION:-10.0.28000.2705}"

# tools downloads the build tools into %LOCALAPPDATA%\sshush-msix-tools and
# checks that Microsoft signed the programs before anything runs them. Only a
# directory that passed, marked by .verified, is used again.
tools() {
  local dir
  dir="$(windows_dir "sshush-msix-tools/$buildtools_version")"
  if [ -f "$dir/.verified" ]; then
    echo "$dir"
    return
  fi
  rm -rf "$dir"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  local url="https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.buildtools/$buildtools_version/microsoft.windows.sdk.buildtools.$buildtools_version.nupkg"
  echo "Fetching Microsoft.Windows.SDK.BuildTools $buildtools_version..." >&2
  curl -sSfL -o "$tmp/buildtools.nupkg" "$url" || die "could not download $url"
  mkdir -p "$tmp/x"
  (cd "$tmp/x" && python3 -I -m zipfile -e "$tmp/buildtools.nupkg" .) >/dev/null
  local bin
  bin="$(echo "$tmp"/x/bin/10.*/x64)"
  [ -f "$bin/makeappx.exe" ] || die "no bin/*/x64/makeappx.exe in the build tools package"
  mkdir -p "$dir"
  cp -r "$bin"/. "$dir/"
  # Under WSL, a file copied onto a Windows drive keeps the archive's mode,
  # which lacks the execute bit WSL needs to start a Windows program.
  find "$dir" -name '*.exe' -exec chmod +x {} +
  local bad
  bad="$(ps "Get-ChildItem -Recurse -Include *.exe,*.dll -Path '$(ps_quote "$(windows_path "$dir")")' | Get-AuthenticodeSignature | Where-Object { \$_.Status -ne 'Valid' -or \$_.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation' } | ForEach-Object { \$_.Path }")"
  if [ -n "$bad" ]; then
    rm -rf "$dir"
    die "not signed by Microsoft, so removed: $bad"
  fi
  touch "$dir/.verified"
  echo "$dir"
}

# find_tool prints the SDK program name.exe to use: the one in $env_var, one
# on PATH, the build tools that tools fetched, or the newest in an installed
# Windows SDK.
#
# The build tools come first: they are the version this script is tested
# with, and always complete. An installed SDK may not be. Installing only
# some of its features (the App Certification Kit, say) gives a bin folder
# with signtool and appxsip.dll but no appxpackaging.dll, and signtool there
# fails on an .msix with "This file format cannot be signed because it is not
# recognized". So an SDK folder without appxpackaging.dll is skipped.
find_tool() {
  local name="$1" env_var="$2"
  if [ -n "${!env_var:-}" ]; then
    echo "${!env_var}"
    return
  fi
  local found=""
  for found in "$name" "$name.exe"; do
    if command -v "$found" >/dev/null 2>&1; then
      command -v "$found"
      return
    fi
  done
  local root candidate
  if command -v powershell.exe >/dev/null 2>&1; then
    root="$(windows_dir "sshush-msix-tools/$buildtools_version")"
    if [ -f "$root/.verified" ] && [ -f "$root/$name.exe" ]; then
      echo "$root/$name.exe"
      return
    fi
  fi
  found=""
  for root in "/c/Program Files (x86)/Windows Kits/10/bin" "/mnt/c/Program Files (x86)/Windows Kits/10/bin"; do
    for candidate in "$root"/10.*/x64/"$name.exe"; do
      [ -f "$candidate" ] && [ -f "$(dirname "$candidate")/appxpackaging.dll" ] && found="$candidate"
    done
  done
  # The glob sorts the SDK versions, so the last one found is the newest.
  if [ -n "$found" ]; then
    echo "$found"
    return
  fi
  return 1
}

pack() {
  local stage_dir="$1" msix="$2"
  local makeappx
  makeappx="$(find_tool makeappx MAKEAPPX)" ||
    die "makeappx not found: run 'scripts/msix.sh tools', install the Windows SDK, or set MAKEAPPX"
  rm -f "$msix"
  mkdir -p "$(dirname "$msix")"
  # makeappx reads and writes through Windows paths; under WSL it packs a
  # copy on the Windows side, which it can read, and the result is moved back.
  local src="$stage_dir" out="$msix" work=""
  if command -v wslpath >/dev/null 2>&1; then
    work="$(windows_dir "sshush-msix-pack")"
    rm -rf "$work"
    mkdir -p "$work/stage"
    cp -r "$stage_dir"/. "$work/stage/"
    src="$work/stage"
    out="$work/package.msix"
  fi
  sdk_tool "$makeappx" pack /o /h SHA256 /d "$(windows_path "$src")" /p "$(windows_path "$out")"
  if [ -n "$work" ]; then
    mv "$out" "$msix"
    rm -rf "$work"
  fi
  echo "Packed $msix"
}

# test_certificate prints the thumbprint of a code signing certificate whose
# subject is the package's publisher, made (self-signed, in the current
# user's store) the first time. A package installs only when its signer's
# subject matches its Publisher exactly.
test_certificate() {
  local publisher
  publisher="$(ps_quote "${MSIX_PUBLISHER:-CN=sshush-dev}")"
  ps "\$c = Get-ChildItem Cert:\\CurrentUser\\My -CodeSigningCert | Where-Object { \$_.Subject -eq '$publisher' -and \$_.NotAfter -gt (Get-Date) } | Select-Object -First 1
      if (-not \$c) {
        \$c = New-SelfSignedCertificate -Type CodeSigningCert -Subject '$publisher' -FriendlyName 'sshush MSIX local test' \`
          -CertStoreLocation Cert:\\CurrentUser\\My -KeyUsage DigitalSignature -NotAfter (Get-Date).AddYears(1) \`
          -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3', '2.5.29.19={text}')
      }
      \$c.Thumbprint"
}

# sign signs msix with the local test certificate, for installing it here as
# the Store would install it. The Store re-signs what it publishes, so this
# certificate never leaves the machine.
sign() {
  local msix="$1"
  local signtool thumbprint
  signtool="$(find_tool signtool SIGNTOOL)" ||
    die "signtool not found: run 'scripts/msix.sh tools', install the Windows SDK, or set SIGNTOOL"
  thumbprint="$(test_certificate | tail -1)"
  [ -n "$thumbprint" ] || die "no test certificate"
  local target="$msix" work=""
  if command -v wslpath >/dev/null 2>&1; then
    work="$(windows_dir "sshush-msix-sign")"
    rm -rf "$work"
    mkdir -p "$work"
    cp "$msix" "$work/package.msix"
    target="$work/package.msix"
  fi
  sdk_tool "$signtool" sign /q /fd SHA256 /sha1 "$thumbprint" "$(windows_path "$target")"
  if [ -n "$work" ]; then
    mv "$target" "$msix"
    rm -rf "$work"
  fi
  echo "Signed $msix with test certificate $thumbprint"
}

# trust adds the test certificate to the machine's Trusted People, which
# installing a package signed by it needs. That store belongs to the machine,
# so Windows asks for an administrator first.
trust() {
  local thumbprint cer
  thumbprint="$(test_certificate | tail -1)"
  cer="$(windows_dir "sshush-msix-test.cer")"
  ps "Export-Certificate -Cert Cert:\\CurrentUser\\My\\$thumbprint -FilePath '$(ps_quote "$(windows_path "$cer")")' | Out-Null"
  ps "Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList '-NoProfile','-Command','Import-Certificate -FilePath ''$(ps_quote "$(ps_quote "$(windows_path "$cer")")")'' -CertStoreLocation Cert:\\LocalMachine\\TrustedPeople'"
  rm -f "$cer"
  echo "Trusted test certificate $thumbprint for this machine. Remove it with, as administrator:"
  echo "  Remove-Item Cert:\\LocalMachine\\TrustedPeople\\$thumbprint"
}

# install installs a signed msix for this user, as a Store install would,
# replacing an installed package of the same name, such as just msix-dev's.
install() {
  local msix="$1"
  local name="${MSIX_IDENTITY_NAME:-sshush.dev}"
  local copy
  copy="$(windows_dir "sshush-msix-install.msix")"
  cp "$msix" "$copy"
  ps "Get-AppxPackage '$(ps_quote "$name")' | Remove-AppxPackage; Add-AppxPackage -Path '$(ps_quote "$(windows_path "$copy")")'"
  rm -f "$copy"
  echo "Installed $msix. Remove it with:"
  echo "  powershell.exe -Command \"Get-AppxPackage $name | Remove-AppxPackage\""
}

# certify runs the Windows App Certification Kit on msix, as Partner Center
# does after a submission, and writes its report to report. It fails unless
# the report's overall result is PASS, listing the tests that did not pass.
#
# The kit installs the package to test it, so it tests a copy signed with the
# test certificate (see sign), which this machine must trust (see trust; it is
# done here when it is not). msix itself is left as it was. The kit needs an
# administrator: Windows asks, unless this already is one (a CI runner).
certify() {
  local msix="$1" report="$2"
  local appcert="${APPCERT:-}" candidate
  if [ -z "$appcert" ]; then
    for candidate in "/c/Program Files (x86)/Windows Kits/10/App Certification Kit/appcert.exe" \
                     "/mnt/c/Program Files (x86)/Windows Kits/10/App Certification Kit/appcert.exe"; do
      [ -f "$candidate" ] && appcert="$candidate"
    done
  fi
  [ -n "$appcert" ] ||
    die "appcert.exe not found: install the Windows SDK with its App Certification Kit, or set APPCERT"
  local work
  work="$(windows_dir "sshush-msix-certify")"
  rm -rf "$work"
  mkdir -p "$work"
  cp "$msix" "$work/package.msix"
  sign "$work/package.msix"
  local thumbprint
  thumbprint="$(test_certificate | tail -1)"
  if [ "$(ps "Test-Path Cert:\\LocalMachine\\TrustedPeople\\$thumbprint")" != "True" ]; then
    trust
  fi
  # An elevated process's output does not come back here, so it keeps a
  # transcript, printed afterwards, and leaves the kit's exit code in a file.
  local w_work
  w_work="$(ps_quote "$(windows_path "$work")")"
  cat > "$work/run.ps1" <<EOF
\$kit = '$(ps_quote "$(windows_path "$appcert")")'
Start-Transcript -Path '$w_work\\run.log' | Out-Null
& \$kit reset
& \$kit test -appxpackagepath '$w_work\\package.msix' -reportoutputpath '$w_work\\report.xml'
Set-Content -Path '$w_work\\exit' -Value \$LASTEXITCODE
Stop-Transcript | Out-Null
EOF
  ps "Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','$w_work\\run.ps1'"
  [ -f "$work/run.log" ] && tr -d '\r' < "$work/run.log"
  [ -f "$work/report.xml" ] || die "the App Certification Kit wrote no report (exit code $(cat "$work/exit" 2>/dev/null || echo unknown))"
  mkdir -p "$(dirname "$report")"
  cp "$work/report.xml" "$report"
  # The report as lines: the overall result, the kit's version, then one
  # line per test of result, optional, name and messages, tab-separated.
  local result
  result="$(ps "\$r = [xml](Get-Content -Raw -Path '$w_work\\report.xml')
      \$r.REPORT.OVERALL_RESULT
      \$r.REPORT.VERSION
      \$r.SelectNodes('//TEST') | ForEach-Object {
        \$m = (\$_.SelectNodes('.//MESSAGE') | ForEach-Object { \$_.TEXT }) -join ' / '
        (\$_.RESULT.'#cdata-section', \$_.OPTIONAL, \$_.NAME, (\$m -replace '\\s+', ' ')) -join [char]9
      }")"
  rm -rf "$work"
  certify_report "$result" "$report"
}

# certify_report reports what certify's report says: every test that did not
# pass, on stdout and, in GitHub Actions, as an annotation (an error for a
# required test, a warning for an optional one) and in the job summary, so
# they show on the run's page, where the store job's reviewer approves it.
# It fails unless the overall result is PASS. A PASS can still have optional
# tests that did not pass: they are warnings, which do not fail it.
certify_report() {
  local result="$1" report="$2"
  local overall version tests
  overall="$(sed -n 1p <<<"$result")"
  version="$(sed -n 2p <<<"$result")"
  tests="$(sed -n '3,$p' <<<"$result")"
  local res opt name msgs label level flagged=0 rows=""
  while IFS=$'\t' read -r res opt name msgs; do
    [ -n "$name" ] || continue
    label="$name"
    [ "$opt" = "TRUE" ] && label="$name (optional)"
    rows+="| $res | $label | ${msgs//|/\\|} |"$'\n'
    [ "$res" = "PASS" ] && continue
    flagged=$((flagged + 1))
    echo "  $res: $label"
    [ -z "$msgs" ] || echo "      $msgs"
    if [ -n "${GITHUB_ACTIONS:-}" ]; then
      level=error
      [ "$opt" = "TRUE" ] && level=warning
      echo "::$level title=App Certification Kit: $name::$res${msgs:+: $msgs}"
    fi
  done <<<"$tests"
  echo "App Certification Kit $version: $overall, $flagged test(s) flagged; report in $report"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    {
      echo "### Windows App Certification Kit: $overall"
      echo
      echo "Kit $version. $flagged of $(grep -c . <<<"$tests") tests did not pass; the full report is the \`wack-report\` artifact."
      echo
      echo "| Result | Test | Messages |"
      echo "|---|---|---|"
      # The tests that did not pass first.
      grep -v '^| PASS |' <<<"$rows" | grep . || true
      grep '^| PASS |' <<<"$rows" || true
    } >> "$GITHUB_STEP_SUMMARY"
  fi
  [ "$overall" = "PASS" ] || die "the package did not pass certification"
}

register() {
  local stage_dir="$1"
  command -v powershell.exe >/dev/null 2>&1 || die "powershell.exe not found: register runs on Windows or under WSL"
  local name="${MSIX_IDENTITY_NAME:-sshush.dev}"
  # Windows refuses to register a changed manifest over an installed package
  # of the same version, so a previous registration of this package goes
  # first, which stops its processes (the tray) and drops its private
  # registry (the tray's settings). It goes before the copy below, too: its
  # running programs would hold the files being replaced.
  powershell.exe -NoProfile -Command "\$ErrorActionPreference = 'Stop'; Get-AppxPackage '$name' | Remove-AppxPackage"
  local target="$stage_dir"
  # Windows will not register a package from inside the WSL file system, so
  # under WSL the staged files are copied to the user's local app data first.
  if command -v wslpath >/dev/null 2>&1; then
    local local_app_data
    local_app_data="$(powershell.exe -NoProfile -Command '[Environment]::GetFolderPath("LocalApplicationData")' | tr -d '\r')"
    [ -n "$local_app_data" ] || die "could not find %LOCALAPPDATA%"
    target="$(wslpath -u "$local_app_data")/sshush-msix-dev"
    rm -rf "$target"
    mkdir -p "$target"
    cp -r "$stage_dir"/. "$target/"
  fi
  local manifest
  manifest="$(windows_path "$target/AppxManifest.xml")"
  powershell.exe -NoProfile -Command "\$ErrorActionPreference = 'Stop'; Add-AppxPackage -Register '$manifest'"
  echo "Registered $(windows_path "$target") for this user. Remove it with:"
  echo "  powershell.exe -Command \"Get-AppxPackage $name | Remove-AppxPackage\""
}

cmd="${1:-}"
shift || true
case "$cmd" in
  version)  [ $# -eq 1 ] || die "usage: version <version>"; msix_version "$1" ;;
  stage)    [ $# -eq 4 ] || die "usage: stage <version> <arch> <bin_dir> <out_dir>"; stage "$@" ;;
  pack)     [ $# -eq 2 ] || die "usage: pack <stage_dir> <msix_file>"; pack "$@" ;;
  register) [ $# -eq 1 ] || die "usage: register <stage_dir>"; register "$@" ;;
  tools)    [ $# -eq 0 ] || die "usage: tools"; tools ;;
  sign)     [ $# -eq 1 ] || die "usage: sign <msix_file>"; sign "$@" ;;
  trust)    [ $# -eq 0 ] || die "usage: trust"; trust ;;
  install)  [ $# -eq 1 ] || die "usage: install <msix_file>"; install "$@" ;;
  certify)  [ $# -eq 2 ] || die "usage: certify <msix_file> <report_xml>"; certify "$@" ;;
  *) die "usage: msix.sh version|stage|pack|register|tools|sign|trust|install|certify ..." ;;
esac
