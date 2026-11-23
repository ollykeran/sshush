# MSIX package (Microsoft Store)

sshush for Windows ships through the Microsoft Store as an MSIX package. The Store signs what it publishes, so the package needs no certificate of ours, and a Store install runs on machines where an unsigned `sshush.exe` from the zip is blocked.

| File | |
|---|---|
| `AppxManifest.xml.in` | the manifest, with `@PLACEHOLDERS@` for the identity and version |
| `Assets/` | logos, drawn by `internal/tray/icons/gen.go` (`go run gen.go msix ../../../packaging/msix/Assets`) |
| `../../scripts/msix.sh` | stages, packs, signs and installs the package; see its header |

## What is in the package

`sshush.exe`, `sshushd.exe` and `sshush-tray.exe` side by side, as two applications:

| Application | Runs | How it is reached |
|---|---|---|
| `Tray` | `sshush-tray.exe` | the Start menu entry "sshush", and at sign-in through the `SshushTray` StartupTask |
| `Cli` | `sshush.exe` | the `sshush` app execution alias, from PowerShell, cmd and WSL |

`sshush start` finds `sshushd.exe` beside itself, inside the package, and the daemon keeps running after the CLI exits.

The config, pidfile, `~\.ssh\config` and the PowerShell profile live outside `AppData`, so MSIX writes them to the real locations, as an unpackaged sshush does. Registry writes are different: a package writes `HKCU` to a private copy of its own. The tray's theme and settings are fine there, but a `Run` key entry would start nothing, so in a package:

- **Sign-in start is the StartupTask.** The tray's "Start at sign-in" checkbox switches it through the Windows Runtime `StartupTask` API (`internal/tray/startuptask_windows.go`). If the user turned it off in Settings > Apps > Startup, Windows only lets Settings turn it back on, so the tray opens that page and says so.
- **The task is on the `Tray` application itself.** Windows looks a task up by the application asking for it, so a task on a separate application could not be switched from the tray the Start menu opens.
- **A sign-in start passes no arguments.** The tray asks Windows how it was activated (`AppInstance.GetActivatedEventArgs`) and stays in the notification area when it was the StartupTask, as `--background` does for the `Run` key.

## Trying it locally

There are two ways, both from WSL or Git Bash.

**Unpacked, while developing** (`just msix-dev`). This needs Developer Mode (Settings > System > For developers) and nothing else. It builds the Windows binaries and registers the staged files as an unsigned package called `sshush.dev`. Running it again replaces the previous registration, along with the package's private settings.

**Packed and signed, as the Store will install it** (`just msix-local`). This builds the `.msix`, signs it with a local test certificate and installs it.
- `makeappx` and `signtool` come from Microsoft's `Microsoft.Windows.SDK.BuildTools` NuGet package. `scripts/msix.sh tools` fetches it into `%LOCALAPPDATA%\sshush-msix-tools` and checks every program in it is signed by Microsoft. No Windows SDK install is needed.
- The certificate is self-signed, made once in your user's certificate store, with your publisher as its subject: a package installs only when the signer matches its `Publisher`.
- Windows installs a package only if it trusts the signer. Allow that once per machine, which asks for an administrator:

  ```sh
  MSIX_PUBLISHER='<your publisher>' bash scripts/msix.sh trust
  ```

Set the four `MSIX_*` variables from Partner Center (see below) when running it, so the local package has the identity the Store package will have.

While a package is installed, `sshush` on `PATH` is the packaged one: the `WindowsApps` folder holding the alias comes early on `PATH`, ahead of a `sshush.exe` you put there yourself. Remove it with:

```powershell
Get-AppxPackage <identity name> | Remove-AppxPackage
```

`internal/tray/startuptask_windows_test.go` checks the StartupTask from inside an installed package; its comment says how to run it there.

## Releases

The `msix` job in `.github/workflows/release.yml` packs the release job's own Windows binaries, so the Store build has the checksums `sshush selftest` checks against, and keeps the result as the `sshush-msix` workflow artifact. It is not attached to the GitHub release: unsigned, nobody could install it from there.

`build.yml` packs a dev build on every push, which validates the manifest against the schema.

## Publishing to the Store

### Once

1. In [Partner Center](https://partner.microsoft.com/dashboard/apps-and-games/overview), create a new app (MSIX or PWA) and reserve its name. **sshush** itself is reserved by someone else (nothing by that name is published, so it is probably an unused reservation; those lapse after three months), so the app needs another name, such as "sshush agent", or "sshush" later if it frees up. The package's display name must be one reserved for it; the command stays `sshush` whatever the name.
2. Under the app's *Product management > Product identity*, copy the values into repository variables, with the name you reserved:

   ```sh
   gh variable set MSIX_DISPLAY_NAME --body '<the reserved name>'
   gh variable set MSIX_IDENTITY_NAME --body '<Package/Identity/Name>'
   gh variable set MSIX_PUBLISHER --body '<Package/Identity/Publisher>'
   gh variable set MSIX_PUBLISHER_DISPLAY_NAME --body '<Package/Properties/PublisherDisplayName>'
   ```

   A build without them uses `sshush.dev` / `CN=sshush-dev`, which the Store refuses. Export the same four for `just msix-local`.

### Version numbers

Every package uploaded for the app needs its own version, higher than the last for an update to reach anyone, and the Store requires the fourth part to be 0. So a pre-release cannot share its release's number, and `scripts/msix.sh` multiplies the patch number by 100 to make room:

| Tag | MSIX version |
|---|---|
| `v0.1.1` | `0.1.199.0` |
| `v0.1.2-rc.1` | `0.1.201.0` (the last number in the suffix, 0 to 98) |
| `v0.1.2-rc.2` | `0.1.202.0` |
| `v0.1.2` | `0.1.299.0` |

A pre-release sorts after the previous release and before its own, so flight testers move onto the release when it is published. Windows shows this number under *Settings > Apps*; `sshush version` and `sshush selftest` use the binaries' own version. `scripts/msix.sh version <tag>` prints the mapping.

### The first release, by hand

1. Tag it. The release workflow's `msix` job produces `sshush-<version>-windows-amd64.msix`; download it from the run's artifacts.
2. In Partner Center, start a submission and upload it under *Packages*.
3. The first submission also needs these, which later ones carry over:
   - pricing: free;
   - category: Developer tools;
   - the age rating questionnaire;
   - a Store listing with at least one screenshot;
   - a privacy policy URL, since sshush can fetch release checksums from GitHub and run an SSH server.
4. `runFullTrust` is a restricted capability, so the submission asks why it is needed. Something like: *sshush is a desktop SSH agent. It runs a background agent process that serves a named pipe to OpenSSH, reads the user's SSH keys from their profile, and provides a command line tool through an app execution alias, none of which is possible in an app container.*

5. To publish the app without anyone finding it yet, set its visibility to private or not discoverable under *Availability*.

Certification can take up to three business days the first time, when someone reads the `runFullTrust` justification; updates usually pass within hours. Once published, the Store updates installs itself.

### Later releases, from CI

The release workflow's `store` job submits each tag's package with the Microsoft Store Developer CLI (`msstore publish`):

| Tag | Goes to |
|---|---|
| `v1.2.3-rc.1`, any tag with a suffix | the package flight `MSSTORE_FLIGHT_ID`, for its testers only |
| `v1.2.3` | everyone |

The submission is committed and certification starts at once; Partner Center emails when it passes or fails. A plain release then goes live as its *Publishing options* say, so set those to publish manually to choose when. A pending draft in Partner Center is replaced by the submission.

The job does nothing until `MSSTORE_APP_ID` is set. To set it up, once the first release is published:

1. **API access.** Associate a Microsoft Entra ID tenant with the Partner Center account (*Account settings > Tenants*), or create one there. Add an Entra application with the *Manager* role (*Account settings > User management > Microsoft Entra applications*) and create a client secret for it.
2. **A flight.** Under *Engagement > Customers > Known user groups*, make a group of the testers' Microsoft accounts (those they sign in to the Store with). Under the app's *Package flights*, create a flight for that group and copy its flight ID.
3. **The `store` environment.** In the repository's *Settings > Environments*, create `store` and add yourself as a required reviewer, so every submission waits for your approval. Put the credentials there, so only that job sees them:

   ```sh
   gh secret set AZURE_AD_TENANT_ID --env store
   gh secret set AZURE_AD_APPLICATION_CLIENT_ID --env store
   gh secret set AZURE_AD_APPLICATION_SECRET --env store
   gh secret set SELLER_ID --env store    # Account settings > Legal info > Developer settings
   gh variable set MSSTORE_FLIGHT_ID --body '<flight ID>'
   gh variable set MSSTORE_APP_ID --body '<Store ID, such as 9N...>'
   ```

   `MSSTORE_APP_ID` goes last: it is what switches the job on.

A suffixed tag still makes a draft GitHub pre-release, so a flight build is seen only by its testers. Each flight submission is certified too, and uses up its version number (see above).

## Not done yet

- **Logo scales.** The logos are only drawn at 100% scale. Sharper Start and taskbar icons at higher DPI need `scale-200` variants and a `resources.pri` from `makepri`.
- **arm64.** Only an x64 package is built. An arm64 one, or an `.msixbundle` holding both, needs the arm64 binaries in the release first.
- **Work machines.** A Store app still installs only where the Store, or the organisation's private store or Intune, allows it. Where IT blocks the Store, ask them to deploy the package or allowlist it.
