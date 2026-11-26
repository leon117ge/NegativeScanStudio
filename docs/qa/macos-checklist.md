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
  Zip it and attach it to the report. Leave scan images and renders out of
  anything that goes into the repo (see 3.3).
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
  - Scans and renders stay on the Mac or the box. Never commit them to the
    repo, attach them to a GitHub issue or PR, or upload them as a CI
    artifact. The repo is public, and public CI uses synthetic data only.
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
  inputs (workflow `macOS QA parity`, on macos-15 and macos-26).
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
Fuji vendor operations 0x9022 and 0x902C. The W6 build adds a small CLI for
this. **The CLI does not exist yet.** In this section, `mactether` and its
subcommands (`list`, `info`, `liveview`, `capture`) are **placeholders (TBD)**.
When W6 lands, use its real command names and flags, and copy them into the
report. The functional checks and their **Expect** lines stay the same.

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
- [ ] **5.3** Run `mactether list`, then `mactether info` (placeholder names, TBD in W6).
  Expect:
  - Manufacturer `FUJIFILM`, model `X-T3`, and the firmware version are shown.
  - The vendor extension is reported, and the vendor op list includes `0x9022`
    and `0x902C`.
  - macOS shows no prompt about another app using the camera.
  Save the output as `tether-info.txt`.
  On failure: save the output. Also run
  `log show --last 5m --predicate 'process CONTAINS "ptpcamera" OR process CONTAINS "mactether"' > ptp-log.txt`
  (replace `mactether` with the real W6 process name).
- [ ] **5.4** Save one live view frame (`mactether liveview`, placeholder, TBD in W6).
  Expect: a JPEG that shows what the camera sees, saved within a few seconds.
  Run it 10 times in a row and record the frame time from the CLI output
  (informational, see the PLAN.md risk on 0x9022 polling speed).
- [ ] **5.5** Capture one RAF (`mactether capture`, placeholder, TBD in W6).
  Expect:
  - The shutter fires once.
  - A `.RAF` of about 50 MB arrives in the output folder, with the exposure and
    ISO as set.
  - The camera ends ready, without a busy or error screen.
  Keep the RAF file name and size.
- [ ] **5.6** Unplug the cable during idle, plug it in again, and run `mactether info`
  again (placeholder, TBD in W6).
  Expect: the camera is found again without restarting the Mac.
- [ ] **5.7** In the app's Camera and Light > Camera page (W9), connect, then
  run live view, a single capture and a 3-frame bracket (for example 0, +2,
  +3 EV).
  Expect: the same results as on Windows. File names have the bracket
  suffixes, and **Stop** aborts a bracket cleanly.

## 6. RGB Light Air over USB serial (W5)

The light is a USB CDC serial device, VID `0483` / PID `5740` (STM32 VCP),
using the RGBAIR API 1.3 protocol. macOS needs no driver. The app uses the
callout device `/dev/cu.usbmodem*`, not `/dev/tty.usbmodem*`.

The test tool is `cmd/rgblightports` on branch `mac/w5-core-libraw-serial-arm64`.
It uses the same protocol code as the app:

```
rgblightports [-port P] [-raw] [-sim] [command [args]...]   (default command: list)
  list                      ports, lights first, labelled "(RGB Light Air, USB 0483:5740)"
  info                      cmd 21: model, serial, HW, FW, protocol
  status                    cmd 20 state + last telemetry if received
  on  r|g|b|all             cmd 02
  off r|g|b|all             cmd 02
  brightness r|g|b|all N    cmd 01, N=1..100 (0 and 101 refused)
  setall R G B              cmd 03, all channels on, confirmed with 20
  -port P   e.g. /dev/cu.usbmodemXXXX or COM5 (default: first port tagged 0483:5740)
  -raw      raw serial hex log to stderr
  -sim      built-in simulated light
  -probe P  = -port P info status
```

Flags go before the commands. Commands chain left to right in one session.

How the tool reports (confirmed by Dev Engineer on `05e973c` with `-sim -raw`;
the behaviour on a real light is still to be confirmed by this checklist):

- Everything, errors included, goes to **stdout**. Only the `-raw` hex log and
  the go tool's own messages go to stderr.
- Every command except `list` prints `port: <name>` as its first line.
- `status` prints one line,
  `state: R <on|off> <1..100>, G <on|off> <n>, B <on|off> <n>, mode <..>, speed <..>, focus <..>`,
  and then `telemetry: {...}` only if the light has sent telemetry. Ignore
  `telemetry:` lines when you compare results.
- `on`, `off` and `brightness` print `on all: ok`, `off g: ok`,
  `brightness r 50: ok`.
- Enable and brightness are independent. `off` keeps the stored brightness.
  `brightness` on a channel that is off changes the value and leaves it off.
- A chain stops at the first error. Commands before it have already been
  applied. Commands after it, a trailing `status` included, do not run.
  Always check the state after an error with a **separate** `status` run.
- Errors exit with code 1. Under `go run`, the go tool also prints
  `exit status 1` to stderr.
- A real light may start in any state. On hardware, compare each `state:` line
  with the first one you took (the baseline, S0). Check what changed, not the
  absolute values. `mode`, `speed` and `focus` must stay as in S0 throughout.

> **Needs 05e973c pushed.** The pushed head `fceae3a` only has `list` and
> `-probe`. `info`, `status`, `on`, `off`, `brightness`, `setall`, `-port`,
> `-raw` and `-sim` come with commit `05e973c`, which is not pushed yet. Until
> it is, run 6.1 to 6.4, the Go test 6.12 (already on `fceae3a`) and the app
> steps 6.13 and 6.14, and mark 6.0 and 6.5 to 6.11 `not in build`. Record the
> commit you tested (`git rev-parse --short HEAD`).

- [ ] **6.0** Build the tool and do a dry run on the simulated light, before
  anything is plugged in:
  ```sh
  cd ~/src/NegativeScanStudio && git fetch origin && git checkout mac/w5-core-libraw-serial-arm64 && git pull --ff-only
  git rev-parse --short HEAD | tee ~/Desktop/nss-qa-$V/light-commit.txt
  go build -o ~/Desktop/nss-qa-$V/rgblightports ./cmd/rgblightports
  cd ~/Desktop/nss-qa-$V
  ./rgblightports -sim status on all status off g status brightness all 1 status \
    brightness r 50 status brightness b 100 status > light-sim.txt; echo "exit $?"
  S='mode sequential, speed fast, focus mixed'
  printf '%s\n' \
    "state: R off 50, G off 50, B off 50, $S" \
    "on all: ok" \
    "state: R on 50, G on 50, B on 50, $S" \
    "off g: ok" \
    "state: R on 50, G off 50, B on 50, $S" \
    "brightness all 1: ok" \
    "state: R on 1, G off 1, B on 1, $S" \
    "brightness r 50: ok" \
    "state: R on 50, G off 1, B on 1, $S" \
    "brightness b 100: ok" \
    "state: R on 50, G off 1, B on 100, $S" > light-sim-expected.txt
  grep -v -e '^port: ' -e '^telemetry: ' light-sim.txt | diff light-sim-expected.txt - && echo SIM-MATCH
  ```
  You can also run the tool from source with `go run ./cmd/rgblightports ...`
  in the repo folder. The rest of this section uses the built `./rgblightports`.
  Expect:
  - `exit 0`.
  - The first line of `light-sim.txt` is `port: <sim port name>`.
  - The other lines are exactly the expected file, so the last line printed
    is `SIM-MATCH`. The simulated light starts with all channels off at 50,
    `mode sequential`, `speed fast` and `focus mixed`.
  - Then check that a refused value sends nothing:
    `./rgblightports -sim -raw brightness r 0 > sim-refuse.txt 2> sim-refuse-raw.log; echo "exit $?"`.
    Expect `exit 1`, the error line from 6.7 in `sim-refuse.txt`, and no TX
    frame in `sim-refuse-raw.log`.

  This shows that the tool works before any hardware is involved.
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
- [ ] **6.3** List the ports:
  ```sh
  ./rgblightports list 2>&1 | tee light-list.txt
  ```
  `./rgblightports` with no command does the same.
  Expect:
  - The light is listed first, as
    `/dev/cu.usbmodemNNNN  (RGB Light Air, USB 0483:5740)`.
  - No `/dev/tty.*` entry carries the label.
  - On Windows the same label follows `COMn`.

  Set the port for the next steps:
  ```sh
  P=/dev/cu.usbmodemNNNN   # the labelled port from the list
  ```
- [ ] **6.4** Probe the light:
  ```sh
  ./rgblightports -probe $P 2>&1 | tee light-probe.txt
  ```
  Expect: `info` shows the model, serial, hardware, firmware and protocol
  (RGBAIR API 1.3), and `status` shows the state of all three channels. Record
  the firmware and protocol in the report.
- [ ] **6.5** On and off, per channel and for all channels. Take the
  baseline first, then run the chain:
  ```sh
  ./rgblightports -port $P status > light-s0.txt
  ./rgblightports -port $P off all status \
    on r status off r status \
    on g status off g status \
    on b status off b status \
    on all status off all status > light-onoff.txt; echo "exit $?"
  ```
  Expect:
  - `exit 0`, and `port: $P` on the first line of both files.
  - Each switch prints its line (`off all: ok`, `on r: ok`, `off r: ok`, and so
    on), followed by a `state:` line.
  - After `off all`, all three are `off`. After `on r`, only R is `on`. After
    `off r`, all are `off` again. The same holds for G and B. After `on all`,
    all three are `on`, and after the final `off all`, all are `off`.
  - Every brightness value equals the one in S0, because on/off does not change
    brightness. `mode`, `speed` and `focus` equal S0.
  - Only the named LEDs light up and go dark.
- [ ] **6.6** Brightness 1, 50 and 100 on each channel and on all channels:
  ```sh
  for ch in r g b all; do
    ./rgblightports -port $P on all brightness $ch 1 status brightness $ch 50 status brightness $ch 100 status
  done > light-brightness.txt
  ./rgblightports -port $P off g brightness g 20 status >> light-brightness.txt
  ./rgblightports -port $P off all status >> light-brightness.txt
  ```
  Expect:
  - Each step prints `brightness <ch> <n>: ok` and then a `state:` line in
    which only the named channel's number has changed, to exactly `<n>`. For
    `all`, all three numbers change. Every channel stays `on`.
  - The LEDs are visibly dimmer at 1 and brightest at 100.
  - `off g brightness g 20` gives `G off 20`. The value is stored, and the
    channel stays off.
  - The final `off all` gives all three `off`, and the numbers are kept.
- [ ] **6.7** Brightness outside the range is refused. Run each refused value
  on its own, then take `status` in a separate run:
  ```sh
  ./rgblightports -port $P status > light-range-before.txt
  ./rgblightports -raw -port $P brightness r 0   > light-range-r0.txt   2> light-range-r0-raw.log;   echo "exit $?"
  ./rgblightports -raw -port $P brightness g 101 > light-range-g101.txt 2> light-range-g101-raw.log; echo "exit $?"
  ./rgblightports -raw -port $P brightness all 0 > light-range-all0.txt 2> light-range-all0-raw.log; echo "exit $?"
  ./rgblightports -port $P status > light-range-after.txt
  ```
  Expect:
  - `exit 1` for each refused value.
  - Stdout is `port: $P`, then exactly
    `error: brightness: R: brightness 0 is outside 1..100 (turn a channel off with its enable flag)`.
    For `g 101` the line is `error: brightness: G: brightness 101 is outside 1..100 (turn a channel off with its enable flag)`.
    For `all 0`, it fails on R first, so the line is the same as for `r 0`.
  - Each `-raw` log has **no TX frame** at all. The value is checked before any
    I/O, so no command 01 is sent.
  - The `state:` line in `light-range-after.txt` is identical to the one in
    `light-range-before.txt`.

  Then show that a chain stops at the error:
  ```sh
  ./rgblightports -port $P off r on r brightness r 0 status > light-range-chain.txt; echo "exit $?"
  ./rgblightports -port $P status >> light-range-chain.txt
  ```
  Expect:
  - `exit 1`.
  - The first run prints `port: $P`, `off r: ok`, `on r: ok`, then the error
    line, and **no** `state:` line.
  - The separate `status` run shows R `on`. The commands before the error were
    applied.
- [ ] **6.8** A non-numeric brightness is refused:
  ```sh
  ./rgblightports -port $P brightness r x > light-badvalue.txt; echo "exit $?"
  ```
  Expect: `exit 1`, and the last line is exactly `error: brightness: bad value "x"`.
  A separate `status` run is unchanged.
- [ ] **6.9** A missing argument is refused:
  ```sh
  ./rgblightports -port $P brightness r > light-missingarg.txt;      echo "exit $?"
  ./rgblightports -port $P setall 30 40 >> light-missingarg.txt;      echo "exit $?"
  ```
  Expect: `exit 1` for both. Each last line has the form
  `error: <cmd>: needs N argument(s)`, for example
  `error: brightness: needs 2 argument(s)` and `error: setall: needs 3 argument(s)`.
  Record the exact lines. Nothing is printed to stderr.
- [ ] **6.10** Set all channels at once:
  ```sh
  ./rgblightports -port $P setall 30 40 50 status > light-setall.txt; echo "exit $?"
  ```
  Expect:
  - `exit 0`.
  - The `state:` line reads `R on 30, G on 40, B on 50`, with `mode`, `speed`
    and `focus` as in S0. The tool confirms `setall` with a command 20 read.
  - All three channels light up.
- [ ] **6.11** Run the chained example and capture the raw serial log:
  ```sh
  ./rgblightports -raw -port $P info status on all brightness g 33 status off all status \
    > light-chain.txt 2> light-serial.log; echo "exit $?"
  ```
  This chain has no failing command, so every `status` in it runs.
  Expect:
  - `exit 0`.
  - The lines appear in order: `port: $P`, the info, a `state:` line, then
    `on all: ok` and `brightness g 33: ok`.
  - The next `state:` line has all three `on` and `G ... 33`. R and B keep
    their values from the first `state:` line.
  - Then `off all: ok`, and a final `state:` line with all three `off` and G
    still 33.
  - `light-serial.log` has the hex bytes sent and received for each command.

  **On failure in any of 6.4 to 6.11:** repeat the failing command on its own
  with `-raw`, and attach both captures. Errors are on stdout and the hex log
  is on stderr:
  `./rgblightports -raw -port $P <commands> > light-fail.txt 2> light-fail-raw.log`.
  If a chain stopped early, the lines after the error are missing on purpose.
  Re-run the remaining commands separately.
- [ ] **6.12** Run the real-light Go test from the repo folder:
  ```sh
  cd ~/src/NegativeScanStudio
  NSS_RGBLIGHT_PORT=$P go test -v -run RealLight ./rgblight 2>&1 | tee ~/Desktop/nss-qa-$V/light-gotest.txt
  ```
  Expect: `TestDarwinRealLight` passes, not skipped. It checks that the port is
  tagged `0483:5740` and logs the light info and state. Without
  `NSS_RGBLIGHT_PORT`, the test is skipped, which does not count as a pass.
- [ ] **6.13** Unplug the light while the app is connected.
  Expect: a clear "disconnected" message, no hang and no crash. After you plug
  it back in and reconnect, status works again. The port number may change,
  so the app must find the port again.
- [ ] **6.14** Film-base leveling with the X-T3 (Light page): run Auto leveling
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
5 X-T3 tether        PASS/FAIL  CLI <name/commit>, live view <ms>/frame, RAF <size>
6 Light serial       PASS/FAIL  port <name>, tool commit <sha>, FW <v>, go test PASS/SKIP
8 Uninstall          PASS/FAIL
Attachments: nss-qa-<version>.zip
```
