# macOS (Apple Silicon) test checklist

Hardware acceptance for NegativeScan Studio on a real Apple Silicon Mac: the two
OFX plugins in DaVinci Resolve, then the app with the Fujifilm X-T3 and the
CuteNewDesign RGB Light Air. CI cannot do these steps (PLAN.md section 4).

How to use it:

- Make one copy per build: `docs/qa/QA_macos_<version>.md`. This follows the
  existing `docs/QA_v0.9x.md` reports. Tick each box and write the measured value
  next to it.
- Every step has an **Expect** line. If the result is different, the step fails.
  Record what the **On failure** line asks for, then go on with the next section
  unless the step says **STOP**.
- Put everything you capture in one folder, `~/Desktop/nss-qa-<version>/`.
  Zip it and attach it to the report.
- Commands are for Terminal (zsh). `$V` is the version under test, for example
  `V=0.90.16`. Paths with spaces are quoted.
- Each section header says which work item delivers the part under test.
  Skip a section when its part is not in the build yet, and mark it
  `not in build`.

---

## 0. Setup and build identity

- [ ] **0.1** Record the Mac. Run:
  ```sh
  mkdir -p ~/Desktop/nss-qa-$V && cd ~/Desktop/nss-qa-$V
  { sw_vers; uname -m; sysctl -n machdep.cpu.brand_string; system_profiler SPDisplaysDataType | grep -E 'Chipset|Metal'; } | tee mac.txt
  ```
  Expect: `arm64`, macOS 13 or later, a `Metal` support line.
- [ ] **0.2** Record the Resolve version (DaVinci Resolve > About DaVinci Resolve)
  and its edition (free or Studio) in the report.
- [ ] **0.3** Record what you downloaded: file names, the GitHub run link, and
  SHA-256 sums.
  ```sh
  shasum -a 256 ~/Downloads/NegativeScanStudio*_macOS*.zip ~/Downloads/NSSPrint2383*_macOS*.zip | tee sha256.txt
  ```
  Expect: each sum matches the `.sha256` file published next to the zip, or the
  value in the CI log. File names carry plain versions, for example
  `..._v0.90.16_macOS_arm64.zip`, with no `-beta` or `-preview`.
  On failure: STOP, report the mismatch, and do not install.
- [ ] **0.4** Quit DaVinci Resolve before installing or removing any plugin.

## 1. Install the OFX plugins (W1, W2, W3)

Both plugins install into `/Library/OFX/Plugins` (admin rights needed):

- `NegativeScanStudio.ofx.bundle`
- `NSSPrint2383.ofx.bundle`

Builds are ad-hoc signed until a Developer ID exists (PLAN.md Q1).
Gatekeeper therefore does not approve them by itself. Resolve skips a
quarantined plugin without telling you, so step 1.5 matters.

- [ ] **1.1** Unzip in Finder (double-click) or with `ditto -x -k <zip> .`.
  Expect: a folder that contains `NegativeScanStudio.ofx.bundle` (or
  `NSSPrint2383.ofx.bundle`), `Install.command`, `README`, `CHANGELOG` and
  `licenses/`. The bundle is itself a folder named `*.ofx.bundle`, not a loose
  `Contents/` folder.
  On failure: list the contents with `find . -maxdepth 3 | tee unzip.txt`.
- [ ] **1.2** Check the downloaded files are quarantined. This shows the
  quarantine clearing in 1.5 is tested for real:
  ```sh
  xattr -p com.apple.quarantine NegativeScanStudio.ofx.bundle 2>&1 | tee quarantine-before.txt
  ```
  Expect: a value like `0083;...;Safari;...`. When nothing is printed, the file
  did not come through a browser download, so note that in the report.
- [ ] **1.3** Check the bundle layout and architecture:
  ```sh
  B=NegativeScanStudio.ofx.bundle
  plutil -p "$B/Contents/Info.plist" | tee plist.txt
  file "$B/Contents/MacOS/"*; lipo -archs "$B/Contents/MacOS/"*
  ```
  Expect:
  - `CFBundlePackageType` is `BNDL`.
  - `CFBundleShortVersionString` and `CFBundleVersion` both equal `$V`.
  - `CFBundleExecutable` names the file in `Contents/MacOS`.
  - The file is `Mach-O 64-bit bundle arm64`.
  Repeat for `NSSPrint2383.ofx.bundle`.
- [ ] **1.4** Check the signature:
  ```sh
  codesign --verify --deep --strict --verbose=2 "$B" 2>&1 | tee codesign-verify.txt
  codesign -dv --verbose=4 "$B" 2>&1 | tee codesign-info.txt
  spctl --assess --type execute -vv "$B" 2>&1 | tee spctl.txt
  ```
  Expect:
  - `codesign --verify` prints `valid on disk` and
    `satisfies its Designated Requirement`.
  - With ad-hoc signing, `codesign -dv` shows `Signature=adhoc` and `spctl`
    reports `rejected`. That is expected and is the reason for step 1.5.
  - With a Developer ID build (if Q1 is yes), `spctl` reports
    `accepted source=Notarized Developer ID`.
  - Any other `codesign` error is a failure.
  On failure: keep the three text files.
- [ ] **1.5** Install. Use the shipped script first:
  ```sh
  open Install.command      # or: bash Install.command
  ```
  If it is blocked ("cannot be opened because it is from an unidentified
  developer"), use Finder: Control-click > Open > Open. You can also go to
  System Settings > Privacy & Security and click Open Anyway. Write down which
  one you needed.

  The manual fallback is the same steps the script does:
  ```sh
  sudo mkdir -p /Library/OFX/Plugins
  sudo rm -rf /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle
  sudo ditto NegativeScanStudio.ofx.bundle /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle
  sudo xattr -dr com.apple.quarantine /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle
  sudo codesign --force --deep --sign - /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle
  ```
  Repeat for `NSSPrint2383.ofx.bundle`.
- [ ] **1.6** Verify the quarantine is gone and the installed copy is intact:
  ```sh
  for b in /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle /Library/OFX/Plugins/NSSPrint2383.ofx.bundle; do
    echo "== $b"; xattr -lr "$b" | grep -c com.apple.quarantine
    codesign --verify --deep --strict "$b" && echo signature-ok
    ls -l "$b/Contents/MacOS"
  done 2>&1 | tee install-verify.txt
  ```
  Expect: a count of `0` and `signature-ok` for each bundle. The owner is
  `root`, and the files can be read by everyone.
  On failure: keep `install-verify.txt` and the output of `ls -le@R "$b"`.
- [ ] **1.7** Check that nothing shipped contains Chinese text. CI already runs
  `ci/qa/check_mac_build.py` on every Mac build (W10), so this is a spot check
  of the installed copies. The macOS `strings` tool lists only ASCII text,
  so use the script:
  ```sh
  python3 check_mac_build.py --cjk-only /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle /Library/OFX/Plugins/NSSPrint2383.ofx.bundle | tee cjk.txt
  ```
  Expect: `RESULT: PASS`.

## 2. DaVinci Resolve loads both plugins (W1, W3)

Resolve paths on macOS:

| What | Path |
|---|---|
| Plugins | `/Library/OFX/Plugins/` |
| Main log | `~/Library/Application Support/Blackmagic Design/DaVinci Resolve/logs/ResolveDebug.txt` |
| OFX plugin cache | `~/Library/Application Support/Blackmagic Design/DaVinci Resolve/OFXPluginCacheV2.xml` |
| Crash reports | `~/Library/Logs/DiagnosticReports/` (files named `Resolve*`) |

- [ ] **2.1** Start Resolve. Open any project and a timeline with a clip.
  Expect: Resolve starts normally, with no crash and no plugin error dialog.
  On failure: STOP. Collect `ResolveDebug.txt`, the newest `Resolve*.ips` in
  `DiagnosticReports`, and a screenshot of any dialog.
- [ ] **2.2** On the Color page, open Effects > Open FX and search for
  `NegativeScan`.
  Expect: both effects are listed in the group **NegativeScan Studio**:
  - **NegativeScan Studio OFX $V**
  - **NSS Print 2383 <2383 version>**
  Screenshot: `resolve-effects-list.png`.
- [ ] **2.3** Drag each effect onto a node.
  Expect:
  - The parameter panel appears, all labels are English, and no `?` boxes or
    blank labels show.
  - The image changes when you move a slider.
  Screenshots: `nss-params.png`, `p2383-params.png`.
- [ ] **2.4** Look for load errors in the log:
  ```sh
  L="$HOME/Library/Application Support/Blackmagic Design/DaVinci Resolve/logs/ResolveDebug.txt"
  grep -niE 'ofx|negativescan|2383|quarantin|code ?sign|dlopen' "$L" | tail -50 | tee resolve-ofx-log.txt
  ```
  Expect: no `failed`, `error`, `rejected` or `not loaded` lines for either
  bundle. If the Metal path logs its first-render properties (PLAN.md section
  8), they appear here, and you keep them.
- [ ] **2.5** Only if a plugin is missing in 2.2: Resolve remembers failed
  plugins. Quit Resolve, move the cache aside, and start it again:
  ```sh
  mv "$HOME/Library/Application Support/Blackmagic Design/DaVinci Resolve/OFXPluginCacheV2.xml" ~/Desktop/nss-qa-$V/
  ```
  Expect: the plugin appears after the restart. If it does, record "needed
  cache reset". Otherwise collect the moved cache file, `ResolveDebug.txt`, and
  `install-verify.txt`, then report a FAIL.
- [ ] **2.6** Check that a saved project reopens. Save the project, quit
  Resolve and open it again.
  Expect: both nodes keep their values, and Resolve has no "missing plugin"
  warning.

## 3. Metal render, Metal vs CPU parity, playback (W2, W3)

The plugin reports **Metal (device name)** or **CPU** (PLAN.md section 2). This
section needs the Mac build to offer two controls:

- **Show render engine**
- **Render engine** with a CPU choice

On Windows 0.90.14 both are hidden parameters. If the Mac build hides them,
mark 3.2 to 3.5 `blocked` and report it.

- [ ] **3.1** Set Resolve to Metal: DaVinci Resolve > Settings > System > Memory
  and GPU > GPU Processing Mode **Metal**. Restart Resolve if asked.
- [ ] **3.2** Click **Show render engine** on each plugin.
  Expect: `Metal (Apple M...)` with the Mac's GPU name, and no Chinese text.
  Screenshot: `engine-metal.png`.
- [ ] **3.3** Render a parity frame with Metal:
  - Use the same source as the Windows tests if you have it (the `0206` scan
    TIFF). If not, use any 16-bit scan TIFF and record its name.
  - Use one node with NegativeScan Studio OFX at its defaults, then a second
    node with NSS Print 2383 at its defaults.
  - On the Deliver page, render a single frame: Format **TIFF**, Codec **RGB 16
    bits**, full resolution, "Render at source resolution".
  - Save it as `frame-metal.tif`.
- [ ] **3.4** Render the same frame with the CPU: set **Render engine** to CPU
  on both nodes, then confirm with **Show render engine** (Expect: `CPU`).
  Render as in 3.3 and save as `frame-cpu.tif`.
- [ ] **3.5** Compare the two frames. `compare_frames.py` is in `ci/qa/` on the
  QA branch and needs only the `python3` from the Command Line Tools:
  ```sh
  python3 compare_frames.py frame-cpu.tif frame-metal.tif --json parity-metal-vs-cpu.json
  ```
  Expect: `PASS`. The limits are:
  - max abs difference at most 8 sixteen-bit codes (1.2e-4 of full scale: the
    plugin's 1e-4 bar plus 16-bit rounding)
  - mean abs difference at most 0.25 code

  Record `max_abs_codes`, `mean_abs_codes` and `identical_values_pct`. A 4K
  frame takes about 20 to 40 seconds.
  On failure: keep both TIFFs, the JSON, and screenshots of both node
  settings.
- [ ] **3.6** Optional, if you have a Windows render of the same frame with the
  same plugin version and the CPU engine:
  ```sh
  python3 compare_frames.py frame-windows-cpu.tif frame-cpu.tif --json parity-mac-vs-windows.json
  ```
  Expect: `PASS` with the same limits. CI already gates this case on synthetic
  inputs (workflow `macOS QA parity`).
- [ ] **3.7** Playback speed. With GPU mode Metal, set the timeline to a 4K or 6K
  scan clip at 24 fps and loop it, with both plugins on.
  - Write down the fps from the Color page viewer's playback indicator, with
    Metal, then with CPU, then with both nodes disabled.
  - Expect: Metal at least as fast as CPU. The numbers are informational: PLAN.md
    does not set an fps target.
  - Also write down Activity Monitor > GPU History during playback, as a
    screenshot (`gpu-history.png`).
- [ ] **3.8** Run fallback. With Metal mode on, quit and restart Resolve and
  render again.
  Expect: still `Metal`. If the plugin ever reports a Metal failure, it falls
  back to `CPU` and keeps rendering (PLAN.md section 2, CPU fallback). Record
  any fallback message from `ResolveDebug.txt`.

## 4. NegativeScan Studio app (W9 onwards)

- [ ] **4.1** Unzip the app zip or open the DMG. Drag
  `NegativeScanStudio.app` to `/Applications`. Check it:
  ```sh
  A=/Applications/NegativeScanStudio.app
  plutil -p "$A/Contents/Info.plist" | grep -E 'CFBundle(ShortVersionString|Version|Identifier)|LSMinimumSystemVersion'
  codesign --verify --deep --strict --verbose=2 "$A"; xattr -p com.apple.quarantine "$A"
  ```
  Expect: plain versions that equal the app version (for example `1.14`),
  `valid on disk`, and the quarantine present after a browser download.
- [ ] **4.2** First launch: Control-click > Open > Open. You can also use System
  Settings > Privacy & Security > Open Anyway. Alternatively, clear the
  quarantine with `xattr -dr com.apple.quarantine "$A"`.
  Expect: the window opens, the title shows `NegativeScan Studio v<version>`,
  and all UI text is English.
  On failure: get the crash log from `~/Library/Logs/NegativeScanStudio/` (PLAN.md
  P4) and `~/Library/Logs/DiagnosticReports/NegativeScanStudio*`.
- [ ] **4.3** Go through every page in tab order. Expect no blank pages and no
  clipped or Chinese text. Screenshots: one per page.

## 5. Fujifilm X-T3 tether over USB (W6)

The Mac transport uses ImageCaptureCore (PLAN.md 3c) and sends raw PTP plus the
Fuji vendor operations 0x9022 and 0x902C. The W6 build adds a small CLI,
`mactether`, with `list`, `info`, live view frame and capture-one-RAF
commands. The exact command names come with that build, so copy them into the
report.

- [ ] **5.1** Camera setup:
  - Put the X-T3 in **M** mode, with the shutter dial and ISO as used for
    scanning.
  - In SET UP > CONNECTION SETTING > PC CONNECTION MODE, choose **USB TETHER
    SHOOTING AUTO**.
  - Use a USB-C data cable, connected straight to the Mac without a hub.
  - Close Image Capture, Photos, Lightroom and FUJIFILM X Acquire.
  Expect: the camera shows the tether connection screen.
- [ ] **5.2** Check that macOS sees the camera:
  ```sh
  system_profiler SPUSBDataType | grep -B3 -A8 -i 'x-t3\|fujifilm' | tee usb-camera.txt
  ```
  Expect: a FUJIFILM device with Vendor ID `0x04cb`.
- [ ] **5.3** Run `mactether list`, then `mactether info`.
  Expect:
  - Manufacturer `FUJIFILM`, model `X-T3`, and the firmware version are shown.
  - The vendor extension is reported, and the vendor op list includes `0x9022`
    and `0x902C`.
  - macOS shows no prompt about another app using the camera.
  Save the output as `tether-info.txt`.
  On failure: save the output. Also run
  `log show --last 5m --predicate 'process CONTAINS "ptpcamera" OR process CONTAINS "mactether"' > ptp-log.txt`.
- [ ] **5.4** Save one live view frame.
  Expect: a JPEG that shows what the camera sees, saved within a few seconds.
  Run it 10 times in a row and record the frame time from the CLI output
  (informational, see the PLAN.md risk on 0x9022 polling speed).
- [ ] **5.5** Capture one RAF.
  Expect:
  - The shutter fires once.
  - A `.RAF` of about 50 MB arrives in the output folder, with the exposure and
    ISO as set.
  - The camera ends ready, without a busy or error screen.
  Keep the RAF file name and size.
- [ ] **5.6** Unplug the cable during idle, plug it in again, and run `info`.
  Expect: the camera is found again without restarting the Mac.
- [ ] **5.7** In the app's Camera and Light > Camera page (W9), connect, then
  run live view, a single capture and a 3-frame bracket (for example 0, +2,
  +3 EV).
  Expect: the same results as on Windows. File names have the bracket
  suffixes, and **Stop** aborts a bracket cleanly.

## 6. RGB Light Air over USB serial (W5)

The light is a USB CDC serial device, VID `0483` / PID `5740` (STM32 VCP),
using the RGBAIR API 1.3 protocol. macOS needs no driver. The app uses the
callout device `/dev/cu.usbmodem*`, not `/dev/tty.usbmodem*`. The W5 build
adds a list-ports command that tags the light as `0483:5740`. Copy its exact
name into the report.

- [ ] **6.1** Plug in the light and check that macOS sees it:
  ```sh
  ls -l /dev/cu.usbmodem* /dev/tty.usbmodem* 2>&1 | tee light-ports.txt
  system_profiler SPUSBDataType | grep -B2 -A10 -i '0x0483\|5740\|stm' | tee -a light-ports.txt
  ```
  Expect:
  - One `/dev/cu.usbmodem<NNNN>` device, with Vendor ID `0x0483` and Product ID
    `0x5740`.
  - Permissions `crw-rw-rw-` for `root wheel`, so no extra group membership or
    `sudo` is needed.
  On failure: try another cable or port, and save `ioreg -p IOUSB -l -w0 > ioreg.txt`.
- [ ] **6.2** Make sure no other process holds the port:
  ```sh
  lsof /dev/cu.usbmodem* 2>/dev/null
  ```
  Expect: no output. If a terminal tool like `screen` holds the port, close
  it.
- [ ] **6.3** Run the W5 list-ports command.
  Expect: the port is listed as `/dev/cu.usbmodemNNNN (RGB Light Air, USB 0483:5740)`.
  The same label is used on Windows with `COMn`.
- [ ] **6.4** In the app's Light page (or the W5 test command), turn on the
  raw serial log, then:
  1. Read status (command 20). Expect: the state of all three channels is
     shown.
  2. Turn R on, then off (command 02). Expect: the red LEDs light up, then go
     dark. The status read back matches.
  3. Set the R brightness to 1, 50 and 100 (command 01). Expect: visibly
     dimmer and brighter, and the status read back matches each value. A
     value of 0 or 101 is refused, because the valid range is 1 to 100 and a
     channel is turned off with its on/off flag.
  4. Set all channels (command 03), R/G/B on at 30/40/50. Expect: all three
     light up, and the status read back shows `on on on 30 40 50`.
  5. Turn all channels off at the end.
  Save the raw serial log as `light-serial.log`.
- [ ] **6.5** Unplug the light while the app is connected.
  Expect: a clear "disconnected" message, no hang and no crash. After you plug
  it back in and reconnect, status works again. The port number may change,
  so the app must find the port again.
- [ ] **6.6** Film-base leveling with the X-T3 (Light page): run Auto leveling
  once.
  Expect: it converges as on Windows. Record the final codes and the time it
  took.

## 7. Optional: Hikrobot MV-CS240 (W7)

- [ ] **7.1** Without the MVS SDK, the app starts and offers the simulator.
- [ ] **7.2** With MVS for macOS installed
  (`/Library/MVS_SDK/lib/libMvCameraControl.dylib` or
  `/Library/Frameworks/MvCameraControl.framework`), the camera is listed, and
  live view and one capture work.

## 8. Uninstall and cleanup

- [ ] **8.1** Quit Resolve and the app.
- [ ] **8.2** Remove the plugins:
  ```sh
  sudo rm -rf /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle /Library/OFX/Plugins/NSSPrint2383.ofx.bundle
  ls /Library/OFX/Plugins
  ```
  Expect: neither bundle is listed. Leave other vendors' plugins alone.
- [ ] **8.3** Make Resolve forget them: move `OFXPluginCacheV2.xml` aside as in
  2.5, then start Resolve.
  Expect: no NegativeScan effects are listed. Projects that used them show the
  missing-plugin state, but they open.
- [ ] **8.4** Remove the app and its data. Only do this if you want a clean
  slate, because it deletes settings and the film-base DB:
  ```sh
  rm -rf /Applications/NegativeScanStudio.app
  ls -d ~/Library/Application\ Support/NegativeScanStudio* ~/Library/Logs/NegativeScanStudio ~/Library/Preferences/*negativescan* 2>/dev/null
  ```
  Expect: back up whatever is listed, then delete it.
- [ ] **8.5** Check that no plugin process is left over:
  `pgrep -fl -i negativescan` gives no output.

## 9. Report

Copy this block into `docs/qa/QA_macos_<version>.md`:

```
Build: NSS OFX <v> / Print 2383 <v> / app <v>, CI run <link>, zip sha256 <...>
Mac: <model>, <chip>, macOS <v>; Resolve <v> <free|Studio>
1 Install            PASS/FAIL  (Open method used: Install.command | Open Anyway | xattr)
2 Resolve load       PASS/FAIL  (cache reset needed: yes/no)
3 Metal engine       PASS/FAIL  device: <name>
3 Parity Metal/CPU   PASS/FAIL  max <n> codes, mean <n>, identical <n>%
3 Parity Mac/Windows PASS/FAIL/not run
3 Playback           Metal <fps>, CPU <fps>, off <fps> @ <res>
5 X-T3 tether        PASS/FAIL  live view <ms>/frame, RAF <size>
6 Light serial       PASS/FAIL  port <name>
8 Uninstall          PASS/FAIL
Attachments: nss-qa-<version>.zip
```
