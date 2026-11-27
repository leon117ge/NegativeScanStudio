# macOS vs Windows parity

This is the parity audit of the macOS app (`macapp/`, Gio) against the Windows app (the walk code in the repository root), for every page except Convert / Develop, which are Windows only by decision. Paths without a folder are root (Windows) files; line numbers are for the tree of this commit.

Status:

- **Ported**: the Mac app has the control with the Windows text and behaviour (the per-page golden and UI tests are listed in `docs/MAC_UI.md`).
- **Ported here**: was missing; ported on `mac/w9-parity`, with tests.
- **Different**: intentionally different on macOS; the reason is in the row.
- **Missing**: not on macOS yet; see "Missing, for a decision" below.

## How the list was made

Every `PushButton`, `CheckBox`, `ComboBox`, `LineEdit`, `NumberEdit`, `TabPage` and `walk.MsgBox` in the root `*.go` files (not tests, not `convertpage.go`, `developpage.go`, `developview.go`, not `third_party/`) was listed with its text, or the label in front of it, and its tooltip. Each was looked up, as an exact string outside comments, in the Mac files of the same page (view, `hdrctl` / `cropctl` controller, copied Windows code). Every row without a hit was checked by hand, and a sample of the hits was checked by hand too. Every `ToolTipText` / `SetToolTipText` literal was checked the same way, and the command-line switches and window-level behaviour (drag and drop, dialog keys, cards) were listed by hand. The walk code has no menus, context menus or keyboard shortcuts.

5 matches of the scan were not controls (slices of existing fields, such as `[]*walk.LineEdit{...}` in `disconnect`) and are left out.

## Summary

| Area | Total | Ported | Ported here | Different | Missing |
| --- | ---: | ---: | ---: | ---: | ---: |
| Controls and message boxes | 293 | 283 | 0 | 10 | 0 |
| Tooltips | 83 | 74 | 5 | 4 | 0 |
| Command-line switches | 12 | 9 | 2 | 1 | 0 |
| Window-level behaviour | 8 | 3 | 0 | 3 | 2 |
| **All** | 396 | 369 | 7 | 18 | 2 |

Message boxes are in-window dialogs on macOS (Gio has one window) and count as ported when the dialog has the Windows title and text.

## Missing, for a decision

Items 1 and 2 are larger than a small, UI-only port, and item 3 needs a root change, so they are listed for the lead instead of being ported here.

1. **Drop files on the window** (`main.go:69` OnDropFiles, routed to Crop, Merge files `hdrpage.go:1955`, Remove hairs `hairpage.go:385` and Batch RAW to TIFF). Gio delivers no file-drop events on macOS, so this needs a native drop target: an Objective-C file in `macapp/` that registers the Gio `NSView` for file URLs (`registerForDraggedTypes`, `performDragOperation`) and posts the paths to the UI goroutine, which then routes them like `main.go`. Size: about 150-250 lines (Objective-C, cgo and Go) plus a routing test. Risks: it touches the Gio view from outside Gio (a subclass or category on its view class can break on a Gio update), it must stay on the main thread, and only a real Mac can test the drag itself (Linux CI can test only the routing). No off-limits file is involved. The Windows hint texts that mention dropping files are kept verbatim meanwhile, and Add files / Add folder / Browse... cover the same jobs.
2. **Enter / Escape in the in-window dialogs** (walk `DefaultButton` / `CancelButton` of the film-base DB save dialog `hdrdb.go:222`, Manage `hdrdb.go:304` and Chart calibration `chartcal.go:64`). Size: about 60-100 lines in the shared panel code (`theme.go` / `dbview.go` / `chartcalview.go`) plus key-event UI tests. Risks: key focus. Enter in a single-line field must not be taken by the panel when the field wants it, and Escape must not close a panel while a background job (Chart calibration Fit) runs. That needs a decision on the focus rules, so it is not in this small-port pass.
3. **Adobe DNG Converter on macOS** (`rawpage.go:667`; counted as Different above). `rawconv` only knows the Windows install path; adding the macOS path changes root code and so the Windows exe, so it waits for the merge to main. Size: small (the path lookup plus a test), but it is not UI-only.

The two known follow-ups in `docs/MAC_UI.md` (the Focus check to Mono trichrome preview restart on Windows, and the `camera/sim.go` anchor) are Windows / shared-code items and are not counted here.

## Command-line switches

| Switch | Windows | Mac | Status | Note |
| --- | --- | --- | --- | --- |
| `--dust light\|strong <tiff> [...]` | `main.go:21, hdrdb.go:528` | `macapp/main.go:38, macapp/dustcli.go` | Ported here | output to stdout only (no message box); same lines and exit codes |
| `--crop <files or folders>` | `main.go:213` | `macapp/appstate/nav.go:141, macapp/main.go applyArgs` | Ported here | the page was ported; the files were dropped until this branch |
| `--develop / --convert <files>` | `main.go:219` | `macapp/appstate/nav.go:120` | Different | Convert is Windows only; ignored, the app opens on Camera & Light > Camera |
| `--light` | `main.go:224` | `macapp/appstate/nav.go:131` | Ported |  |
| `--tri` | `main.go:229` | `macapp/appstate/nav.go:135` | Ported |  |
| `--mono` | `main.go:233` | `macapp/appstate/nav.go:137` | Ported |  |
| `--focus` | `main.go:237` | `macapp/appstate/nav.go:139` | Ported |  |
| `--camera` | `main.go:244` | `macapp/appstate/nav.go:129` | Ported |  |
| `--white` | `main.go:246` | `macapp/appstate/nav.go:133` | Ported |  |
| `--merge` | `main.go:248` | `macapp/appstate/nav.go:144` | Ported |  |
| `--hairs` | `main.go:251` | `macapp/appstate/nav.go:146` | Ported |  |
| `--batch` | `main.go:254` | `macapp/appstate/nav.go:148` | Ported |  |

The Mac app also has `-selftest-runloop` (a CI check of the macOS run loop); Windows has no counterpart.

## Window-level behaviour

| Item | Windows | Mac | Status | Note |
| --- | --- | --- | --- | --- |
| Drop files on the window (routed by page: Crop, Merge files, Remove hairs, Batch) | `main.go:69, hdrpage.go:1955, hairpage.go:385` | - | Missing | big list item 1 |
| Enter / Escape press the dialog default / cancel button | `hdrdb.go:222, hdrdb.go:304, chartcal.go:64` | - | Missing | big list item 2 |
| Collapsible cards, Reset arrow in the header | `card.go:81` | `macapp/theme.go:566` | Ported |  |
| About button and text | `main.go:112, about.go:9` | `macapp/shell.go:266` | Ported | in-window dialog |
| Crash log (crash_log.txt) | `crash.go:17` | `macapp/crashlog/crashlog.go` | Ported | file in the Mac config folder |
| Copy the old "Film Base Meter" settings on first start | `main.go:16` | - | Different | Windows-only legacy install; there was never a Mac Film Base Meter |
| Window size 1280x860, minimum 640x420 | `main.go:65-66` | `macapp/main.go:52` | Different | 1280x800, minimum 1024x640 so the two-column pages fit (MAC_UI.md Layout) |
| Develop white-balance presets (wb_config.txt), calibrated stocks | `wb.go:49, previewpresets.go:5` | - | Different | used only by developpage.go (Convert/Develop) |

## Controls by page

### App window and page bar

5 items: Ported 3, Different 2.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `main.go:36` | TabPage |  | `macapp/appstate/nav.go:21` | Ported | module pages; Convert module is Windows only |
| `main.go:112` | PushButton | About | `macapp/shell.go:266` | Ported |  |
| `main.go:195` | MsgBox | 500T colour cast | - | Different | 500T "Send to Develop" hook message; the button is removed on Mac |
| `main.go:198` | MsgBox | Film base | - | Different | Send base to Develop hook message; the buttons are removed on Mac |
| `about.go:10` | MsgBox | About | `macapp/shell.go:266` | Ported | in-window dialog |

### Camera & Light > Camera (camera card, Frame 0, film-base DB)

24 items: Ported 24.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `hdrdb.go:227` | ComboBox | Action | `macapp/dbview.go:131` | Ported |  |
| `hdrdb.go:229` | LineEdit | New stock name | `macapp/dbview.go:134` | Ported |  |
| `hdrdb.go:231` | ComboBox | Existing stock | `macapp/dbview.go:137` | Ported |  |
| `hdrdb.go:233` | ComboBox | Update mode | `macapp/dbview.go:140` | Ported |  |
| `hdrdb.go:237` | LineEdit | Light source | `macapp/dbview.go:151` | Ported |  |
| `hdrdb.go:240` | PushButton | Save | `macapp/dbview.go:155` | Ported |  |
| `hdrdb.go:241` | PushButton | Cancel | `macapp/dbview.go:155` | Ported |  |
| `hdrdb.go:309` | ComboBox | Film stock | `macapp/dbview.go:175` | Ported | Manage: film stock combo |
| `hdrdb.go:313` | LineEdit | Name | `macapp/dbview.go:190` | Ported |  |
| `hdrdb.go:315` | PushButton | Rename | `macapp/dbview.go:198` | Ported |  |
| `hdrdb.go:322` | MsgBox | Rename | `macapp/dbview.go:198` | Ported | in-window dialog |
| `hdrdb.go:326` | MsgBox | Rename | `macapp/dbview.go:198` | Ported | in-window dialog |
| `hdrdb.go:330` | MsgBox | Rename | `macapp/dbview.go:198` | Ported | in-window dialog |
| `hdrdb.go:338` | PushButton | Delete | `macapp/dbview.go:198` | Ported |  |
| `hdrdb.go:343` | MsgBox | Delete | `macapp/dbview.go:198` | Ported | in-window dialog |
| `hdrdb.go:348` | MsgBox | Delete | `macapp/dbview.go:198` | Ported | in-window dialog |
| `hdrdb.go:354` | PushButton | Close | `macapp/dbview.go:199` | Ported |  |
| `hdrdb.go:359` | MsgBox | Film-base DB | `macapp/dbview.go:170` | Ported | in-window dialog |
| `hdrdb.go:385` | MsgBox | Record base | `macapp/hdrview.go:397` | Ported | in-window dialog |
| `hdrdb.go:435` | MsgBox | Record base | `macapp/hdrview.go:397` | Ported | in-window dialog |
| `hdrdb.go:473` | MsgBox | Save base to DB | `macapp/hdrview.go:399` | Ported | in-window dialog |
| `hdrdb.go:479` | MsgBox | Save base to DB | `macapp/hdrview.go:399` | Ported | in-window dialog |
| `hdrdb.go:499` | MsgBox | Save last frame 0 to DB | `macapp/hdrview.go:401` | Ported | in-window dialog |
| `hdrdb.go:503` | MsgBox | Save last frame 0 to DB | `macapp/hdrview.go:401` | Ported | in-window dialog |

### Camera & Light > Camera, HDR Scan > White light scan, Crop & Merge > Merge files (hdrpage.go)

73 items: Ported 69, Different 4.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `hdrpage.go:561` | PushButton | Start live view | `macapp/hdrctl/ctl.go:249` | Ported |  |
| `hdrpage.go:562` | PushButton | Reset to Auto | `macapp/hdrview.go:214` | Ported |  |
| `hdrpage.go:826` | MsgBox | Send base to Develop | - | Different | message of the removed Send base to Develop |
| `hdrpage.go:842` | MsgBox | Open in Develop | - | Different | message of the removed Open in Develop |
| `hdrpage.go:969` | MsgBox | Connect camera | `macapp/hdrctl/ctl.go:361` | Ported | in-window dialog |
| `hdrpage.go:1024` | MsgBox | Set the camera to M | `macapp/hdrctl/ctl.go:407` | Ported | in-window dialog |
| `hdrpage.go:1033` | MsgBox | Set the exposure on the camera | `macapp/hdrctl/ctl.go:418` | Ported | in-window dialog |
| `hdrpage.go:1091` | MsgBox | Camera diagnostics | `macapp/hdrview.go:341` | Ported | in-window dialog |
| `hdrpage.go:1492` | MsgBox | Meter base | `macapp/hdrctl/ctl.go:250` | Ported | in-window dialog |
| `hdrpage.go:1620` | MsgBox | Frame 0 | `macapp/hdrview.go:372` | Ported | in-window dialog |
| `hdrpage.go:1671` | MsgBox | Test shot | `macapp/hdrview.go:342` | Ported | in-window dialog |
| `hdrpage.go:1724` | MsgBox | Test shot | `macapp/hdrview.go:342` | Ported | in-window dialog |
| `hdrpage.go:1746` | MsgBox | Output folder | `macapp/hdrctl/ctl.go:991` | Ported | in-window dialog |
| `hdrpage.go:1997` | MsgBox | Merge files | `macapp/hdrctl/ctl.go:1089` | Ported | in-window dialog |
| `hdrpage.go:2002` | MsgBox | Merge files | `macapp/hdrctl/ctl.go:1089` | Ported | in-window dialog |
| `hdrpage.go:2048` | ComboBox |  | `macapp/hdrview.go:331` | Ported | camera device combo |
| `hdrpage.go:2049` | PushButton | Refresh | `macapp/hdrview.go:332` | Ported |  |
| `hdrpage.go:2050` | PushButton | Chart calibration... | `macapp/hdrview.go:333` | Ported |  |
| `hdrpage.go:2053` | PushButton | Connect | `macapp/hdrctl/ctl.go:247` | Ported |  |
| `hdrpage.go:2054` | PushButton | Camera diagnostics | `macapp/hdrview.go:341` | Ported |  |
| `hdrpage.go:2055` | PushButton | Test shot | `macapp/hdrview.go:342` | Ported |  |
| `hdrpage.go:2068` | ComboBox | Base level | `macapp/hdrview.go:361` | Ported |  |
| `hdrpage.go:2071` | ComboBox | Base area | `macapp/hdrview.go:365` | Ported |  |
| `hdrpage.go:2073` | ComboBox | Slowest shutter | `macapp/hdrview.go:368` | Ported |  |
| `hdrpage.go:2077` | ComboBox | Frame 0 | `macapp/hdrview.go:372` | Ported |  |
| `hdrpage.go:2080` | ComboBox | ISO | `macapp/hdrview.go:376` | Ported |  |
| `hdrpage.go:2084` | ComboBox | Base shutter | `macapp/hdrview.go:381` | Ported |  |
| `hdrpage.go:2088` | ComboBox | Film stock | `macapp/hdrview.go:385` | Ported |  |
| `hdrpage.go:2091` | ComboBox | Tol. | `macapp/hdrview.go:389` | Ported |  |
| `hdrpage.go:2095` | PushButton | Record base | `macapp/hdrview.go:397` | Ported |  |
| `hdrpage.go:2097` | PushButton | Save base to DB | `macapp/hdrview.go:399` | Ported |  |
| `hdrpage.go:2099` | PushButton | Save last frame 0 to DB | `macapp/hdrview.go:401` | Ported |  |
| `hdrpage.go:2101` | PushButton | Manage | `macapp/hdrview.go:403` | Ported |  |
| `hdrpage.go:2103` | PushButton | Meter base | `macapp/hdrctl/ctl.go:250` | Ported |  |
| `hdrpage.go:2106` | LineEdit | Camera now | `macapp/hdrview.go:465` | Ported |  |
| `hdrpage.go:2107` | LineEdit | Camera now | `macapp/hdrview.go:465` | Ported |  |
| `hdrpage.go:2108` | LineEdit | Camera now | `macapp/hdrview.go:465` | Ported |  |
| `hdrpage.go:2110` | LineEdit | not locked | `macapp/hdrctl/ctl.go:139` | Ported |  |
| `hdrpage.go:2111` | LineEdit | Frame 0 | `macapp/hdrview.go:372` | Ported |  |
| `hdrpage.go:2114` | LineEdit | Brighter | `macapp/hdrview.go:468` | Ported |  |
| `hdrpage.go:2116` | LineEdit | Base level | `macapp/hdrview.go:361` | Ported |  |
| `hdrpage.go:2117` | PushButton | Send base to Develop | - | Different | "Send base to Develop": only consumer is the Develop/Convert hook (main.go SendBase); removed on Mac |
| `hdrpage.go:2120` | LineEdit | Base R / G / B | `macapp/hdrview.go:474` | Ported |  |
| `hdrpage.go:2121` | LineEdit | Base R / G / B | `macapp/hdrview.go:474` | Ported |  |
| `hdrpage.go:2122` | LineEdit | Base R / G / B | `macapp/hdrview.go:474` | Ported |  |
| `hdrpage.go:2124` | CheckBox | Film base for metadata: Last leveling (after profile) | `macapp/hdrview.go:420` | Ported |  |
| `hdrpage.go:2147` | LineEdit | Save to | `macapp/hdrview.go:497` | Ported |  |
| `hdrpage.go:2148` | PushButton | ... | `macapp/hdrview.go:507` | Ported | Save to folder "..." button |
| `hdrpage.go:2159` | LineEdit | Roll name | `macapp/hdrview.go:520` | Ported |  |
| `hdrpage.go:2169` | ComboBox | Bracket | `macapp/hdrview.go:535` | Ported |  |
| `hdrpage.go:2172` | ComboBox | Frame TIFFs | `macapp/hdrview.go:539` | Ported |  |
| `hdrpage.go:2174` | ComboBox | RAF files | `macapp/hdrview.go:542` | Ported |  |
| `hdrpage.go:2176` | ComboBox | Alignment | `macapp/hdrview.go:545` | Ported |  |
| `hdrpage.go:2178` | ComboBox | Merge | `macapp/hdrview.go:548` | Ported |  |
| `hdrpage.go:2180` | ComboBox | Compression | `macapp/hdrview.go:551` | Ported |  |
| `hdrpage.go:2182` | ComboBox | Dust removal | `macapp/hdrview.go:554` | Ported |  |
| `hdrpage.go:2185` | PushButton | Capture HDR | `macapp/hdrctl/ctl.go:254` | Ported |  |
| `hdrpage.go:2187` | PushButton | Stop | `macapp/hdrview.go:566` | Ported |  |
| `hdrpage.go:2196` | TabPage | Camera | `macapp/hdrview.go:326` | Ported |  |
| `hdrpage.go:2209` | TabPage | White light scan | `macapp/shell.go:280` | Ported |  |
| `hdrpage.go:2234` | TabPage | Merge files | `macapp/hdrctl/ctl.go:1089` | Ported |  |
| `hdrpage.go:2243` | LineEdit | Frame 0 | `macapp/hdrview.go:372` | Ported |  |
| `hdrpage.go:2244` | PushButton | Browse... | `macapp/mergeview.go:86` | Ported |  |
| `hdrpage.go:2246` | LineEdit | Brighter frame (+2 or +3 EV) | `macapp/mergeview.go:59` | Ported |  |
| `hdrpage.go:2247` | PushButton | Browse... | `macapp/mergeview.go:86` | Ported |  |
| `hdrpage.go:2249` | LineEdit | Frame +3 or +4 EV (optional) | `macapp/mergeview.go:59` | Ported |  |
| `hdrpage.go:2250` | PushButton | Browse... | `macapp/mergeview.go:86` | Ported |  |
| `hdrpage.go:2252` | ComboBox | Alignment | `macapp/hdrview.go:545` | Ported |  |
| `hdrpage.go:2255` | ComboBox | Merge | `macapp/hdrview.go:548` | Ported |  |
| `hdrpage.go:2258` | ComboBox | Compression | `macapp/hdrview.go:551` | Ported |  |
| `hdrpage.go:2261` | ComboBox | Dust removal | `macapp/hdrview.go:554` | Ported |  |
| `hdrpage.go:2266` | PushButton | Merge to HDR TIFF | `macapp/mergeview.go:100` | Ported |  |
| `hdrpage.go:2267` | PushButton | Open in Develop | - | Different | "Open in Develop" opens Convert only; removed on Mac (Convert is Windows only) |

### Chart calibration (Camera card)

23 items: Ported 23.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `chartcal.go:69` | PushButton | Open chart TIFF... | `macapp/chartcalview.go:104` | Ported |  |
| `chartcal.go:70` | PushButton | Use last scan | `macapp/chartcalview.go:105` | Ported |  |
| `chartcal.go:75` | PushButton | Load reference (CGATS)... | `macapp/chartcalview.go:108` | Ported |  |
| `chartcal.go:83` | PushButton | Auto-detect | `macapp/chartcalview.go:119` | Ported |  |
| `chartcal.go:84` | PushButton | Rotate 180° | `macapp/chartcalview.go:120` | Ported |  |
| `chartcal.go:89` | PushButton | Reset grid | `macapp/chartcalview.go:121` | Ported |  |
| `chartcal.go:91` | PushButton | Fit | `macapp/chartcalview.go:128` | Ported |  |
| `chartcal.go:96` | LineEdit | XT3 trichrome | `macapp/hdrctl/chartcal.go:83` | Ported |  |
| `chartcal.go:97` | PushButton | Save as camera profile | `macapp/chartcalview.go:140` | Ported |  |
| `chartcal.go:102` | ComboBox | Active camera profile | `macapp/chartcalview.go:144` | Ported |  |
| `chartcal.go:103` | CheckBox | Enable | `macapp/chartcalview.go:154` | Ported |  |
| `chartcal.go:105` | NumberEdit | Strength | `macapp/chartcalview.go:160` | Ported |  |
| `chartcal.go:106` | PushButton | Delete profile | `macapp/chartcalview.go:169` | Ported |  |
| `chartcal.go:108` | PushButton | Close | `macapp/chartcalview.go:171` | Ported |  |
| `chartcal.go:113` | MsgBox | Chart calibration | `macapp/chartcalview.go:100` | Ported | in-window dialog |
| `chartcal.go:173` | MsgBox | Delete profile | `macapp/chartcalview.go:169` | Ported | in-window dialog |
| `chartcal.go:198` | MsgBox | Use last scan | `macapp/chartcalview.go:105` | Ported | in-window dialog |
| `chartcal.go:244` | MsgBox | Reference | `macapp/hdrctl/chartcal.go:240` | Ported | in-window dialog |
| `chartcal.go:250` | MsgBox | Open chart | `macapp/hdrctl/chartcal.go:257` | Ported | in-window dialog |
| `chartcal.go:452` | MsgBox | Fit | `macapp/chartcalview.go:128` | Ported | in-window dialog |
| `chartcal.go:513` | MsgBox | Save | `macapp/hdrctl/chartcal.go:540` | Ported | in-window dialog |
| `chartcal.go:519` | MsgBox | Save | `macapp/hdrctl/chartcal.go:540` | Ported | in-window dialog |
| `chartcal.go:528` | MsgBox | Save | `macapp/hdrctl/chartcal.go:540` | Ported | in-window dialog |

### Camera & Light > Light (light, leveling, scan config presets, 500T)

64 items: Ported 62, Different 2.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `lightpage.go:388` | MsgBox | RGB light | `macapp/hdrctl/light.go:338` | Ported | in-window dialog |
| `lightpage.go:408` | MsgBox | RGB light | `macapp/hdrctl/light.go:338` | Ported | in-window dialog |
| `lightpage.go:576` | MsgBox | RGB light | `macapp/hdrctl/light.go:338` | Ported | in-window dialog |
| `lightpage.go:594` | MsgBox | RGB light | `macapp/hdrctl/light.go:338` | Ported | in-window dialog |
| `lightpage.go:756` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:767` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:787` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:794` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:801` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:814` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:818` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:825` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:884` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:1091` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:1155` | MsgBox | Auto film-base leveling | `macapp/lightview.go:382` | Ported | in-window dialog |
| `lightpage.go:1344` | CheckBox |  | `macapp/lightview.go:218` | Ported | "<channel> on" check boxes |
| `lightpage.go:1345` | LineEdit | Brightness 1..100. Switch a channel off with its checkbox (brightne... | `macapp/lightview.go:254` | Ported |  |
| `lightpage.go:1355` | TabPage | Light | `macapp/appstate/nav.go:21` | Ported | Light sub-page (Camera & Light > Light) |
| `lightpage.go:1389` | ComboBox | Serial ports. The light (USB 0483:5740) is found automatically and ... | `macapp/lightview.go:186` | Ported |  |
| `lightpage.go:1391` | PushButton | Refresh | `macapp/lightview.go:187` | Ported |  |
| `lightpage.go:1392` | PushButton | Connect | `macapp/hdrctl/light.go:235` | Ported |  |
| `lightpage.go:1406` | PushButton | Apply | `macapp/lightview.go:231` | Ported |  |
| `lightpage.go:1408` | PushButton | Read state | `macapp/lightview.go:232` | Ported |  |
| `lightpage.go:1411` | PushButton | All off | `macapp/lightview.go:233` | Ported |  |
| `lightpage.go:1423` | ComboBox | Leveling camera | `macapp/lightview.go:289` | Ported |  |
| `lightpage.go:1429` | ComboBox | ISO | `macapp/lightview.go:312` | Ported |  |
| `lightpage.go:1432` | ComboBox | Shutter | `macapp/lightview.go:321` | Ported |  |
| `lightpage.go:1439` | NumberEdit | Exposure | `macapp/lightview.go:301` | Ported |  |
| `lightpage.go:1451` | ComboBox | Base level | `macapp/lightview.go:331` | Ported |  |
| `lightpage.go:1456` | LineEdit | ± | `macapp/lightview.go:337` | Ported |  |
| `lightpage.go:1459` | LineEdit | Max rounds | `macapp/lightview.go:338` | Ported |  |
| `lightpage.go:1461` | LineEdit | Settle | `macapp/lightview.go:339` | Ported |  |
| `lightpage.go:1466` | ComboBox | Film base | `macapp/lightview.go:344` | Ported |  |
| `lightpage.go:1469` | CheckBox | Apply camera profile | `macapp/lightview.go:353` | Ported |  |
| `lightpage.go:1472` | PushButton | Start leveling | `macapp/lightview.go:361` | Ported |  |
| `lightpage.go:1474` | PushButton | Stop | `macapp/lightview.go:363` | Ported |  |
| `lightpage.go:1483` | PushButton | Copy | `macapp/lightview.go:378` | Ported |  |
| `levelingexport.go:10` | MsgBox | Copy leveling | `macapp/hdrctl/light_level.go:509` | Different | Copy: the Gio clipboard write has no error result, so there is no failure message |
| `configpage.go:33` | ComboBox |  | `macapp/lightview.go:406` | Ported | preset combo |
| `configpage.go:34` | PushButton | Load preset | `macapp/lightview.go:407` | Ported |  |
| `configpage.go:36` | PushButton | Delete | `macapp/lightview.go:409` | Ported |  |
| `configpage.go:38` | LineEdit | Name | `macapp/lightview.go:413` | Ported |  |
| `configpage.go:39` | LineEdit | Film stock | `macapp/lightview.go:414` | Ported |  |
| `configpage.go:40` | LineEdit | Notes | `macapp/lightview.go:415` | Ported |  |
| `configpage.go:42` | PushButton | Save preset | `macapp/lightview.go:419` | Ported |  |
| `configpage.go:44` | PushButton | Save as file... | `macapp/lightview.go:421` | Ported |  |
| `configpage.go:45` | PushButton | Load from file... | `macapp/lightview.go:422` | Ported |  |
| `configpage.go:46` | CheckBox | Apply when chosen | `macapp/lightview.go:424` | Ported |  |
| `configpage.go:228` | MsgBox | Save preset | `macapp/lightview.go:419` | Ported | in-window dialog |
| `configpage.go:231` | MsgBox | Save preset | `macapp/lightview.go:419` | Ported | in-window dialog |
| `configpage.go:236` | MsgBox | Save preset | `macapp/lightview.go:419` | Ported | in-window dialog |
| `configpage.go:247` | MsgBox | Delete preset | `macapp/hdrctl/light_presets.go:191` | Ported | in-window dialog |
| `configpage.go:253` | MsgBox | Delete preset | `macapp/hdrctl/light_presets.go:191` | Ported | in-window dialog |
| `configpage.go:268` | MsgBox | Save config as file | `macapp/hdrctl/light_presets.go:215` | Ported | in-window dialog |
| `configpage.go:280` | MsgBox | Save config as file | `macapp/hdrctl/light_presets.go:215` | Ported | in-window dialog |
| `configpage.go:293` | MsgBox | Load config from file | `macapp/hdrctl/light_presets.go:241` | Ported | in-window dialog |
| `configpage.go:298` | MsgBox | Load config from file | `macapp/hdrctl/light_presets.go:241` | Ported | in-window dialog |
| `configpage.go:320` | MsgBox | Load preset | `macapp/lightview.go:407` | Ported | in-window dialog |
| `castpage.go:50` | MsgBox | 500T colour cast | `macapp/hdrctl/light_presets.go:367` | Ported | in-window dialog |
| `castpage.go:64` | ComboBox | 500T is balanced for 3200K. Set the ORIGINAL scene light, not the s... | `macapp/lightview.go:613` | Ported |  |
| `castpage.go:74` | LineEdit |  | `macapp/lightview.go:619` | Ported | 500T Kelvin field |
| `castpage.go:75` | CheckBox | Enable | `macapp/lightview.go:622` | Ported |  |
| `castpage.go:75` | PushButton | Calculate | `macapp/lightview.go:625` | Ported |  |
| `castpage.go:79` | PushButton | Send to Develop | - | Different | Develop/Convert hook only (main.go toDevelop shows a message, stores nothing); removed on Mac, see MAC_UI.md Light page |

### HDR Scan > Trichrome

9 items: Ported 8, Different 1.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `tripage.go:473` | MsgBox | Output folder | `macapp/hdrctl/tri.go:288` | Ported | in-window dialog |
| `tripage.go:698` | TabPage | Trichrome | `macapp/triview.go:97` | Ported |  |
| `tripage.go:720` | ComboBox | Mode | `macapp/triview.go:116` | Ported |  |
| `tripage.go:731` | ComboBox | Bracket per channel | `macapp/triview.go:119` | Ported |  |
| `tripage.go:743` | PushButton | Capture trichrome | `macapp/hdrctl/tri.go:117` | Ported |  |
| `tripage.go:745` | PushButton | Stop | `macapp/triview.go:131` | Ported |  |
| `tripage.go:749` | PushButton | Meter base | `macapp/triview.go:142` | Ported |  |
| `tripage.go:751` | PushButton | Send base to Develop | - | Different | "Send base to Develop": Develop/Convert hook only; removed on Mac |
| `tripage.go:755` | CheckBox | Film base metadata: Last leveling | `macapp/triview.go:160` | Ported |  |

### HDR Scan > Mono trichrome

28 items: Ported 28.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `monopage.go:363` | MsgBox | Hikrobot camera | `macapp/hdrctl/mono.go:406` | Ported | in-window dialog |
| `monopage.go:735` | MsgBox | Output folder | `macapp/hdrctl/mono.go:772` | Ported | in-window dialog |
| `monopage.go:843` | MsgBox | Mono trichrome | `macapp/hdrctl/mono.go:942` | Ported | in-window dialog |
| `monopage.go:987` | TabPage | Mono trichrome | `macapp/hdrctl/mono.go:942` | Ported |  |
| `monopage.go:1001` | CheckBox | Center mark | `macapp/monoview.go:95` | Ported |  |
| `monopage.go:1021` | ComboBox | SDK: - | `macapp/hdrctl/mono.go:112` | Ported |  |
| `monopage.go:1022` | PushButton | Refresh | `macapp/monoview.go:269` | Ported |  |
| `monopage.go:1025` | PushButton | Connect | `macapp/hdrctl/mono.go:263` | Ported |  |
| `monopage.go:1031` | ComboBox | Preset | `macapp/monoview.go:282` | Ported |  |
| `monopage.go:1035` | ComboBox | Pixel format | `macapp/monoview.go:285` | Ported |  |
| `monopage.go:1038` | NumberEdit | Exposure | `macapp/monoview.go:289` | Ported |  |
| `monopage.go:1048` | NumberEdit | Gain | `macapp/monoview.go:305` | Ported |  |
| `monopage.go:1051` | NumberEdit | Black level | `macapp/monoview.go:308` | Ported |  |
| `monopage.go:1055` | ComboBox | Trigger | `macapp/monoview.go:312` | Ported |  |
| `monopage.go:1058` | CheckBox | Enable camera gamma | `macapp/monoview.go:316` | Ported |  |
| `monopage.go:1060` | CheckBox | Full frame | `macapp/monoview.go:320` | Ported |  |
| `monopage.go:1064` | NumberEdit | Width | `macapp/monoview.go:350` | Ported |  |
| `monopage.go:1066` | NumberEdit | Height | `macapp/monoview.go:350` | Ported |  |
| `monopage.go:1068` | NumberEdit | Offset X | `macapp/monoview.go:350` | Ported |  |
| `monopage.go:1070` | NumberEdit | Offset Y | `macapp/monoview.go:350` | Ported |  |
| `monopage.go:1072` | PushButton | Apply camera settings | `macapp/monoview.go:354` | Ported |  |
| `monopage.go:1115` | CheckBox | Correct lateral chromatic aberration | `macapp/monoview.go:379` | Ported |  |
| `monopage.go:1117` | CheckBox | Auto-meter each channel | `macapp/monoview.go:384` | Ported |  |
| `monopage.go:1128` | ComboBox | Mode | `macapp/monoview.go:392` | Ported |  |
| `monopage.go:1132` | ComboBox | HDR bracket | `macapp/monoview.go:396` | Ported |  |
| `monopage.go:1138` | NumberEdit | Frames per exposure | `macapp/monoview.go:400` | Ported |  |
| `monopage.go:1143` | PushButton | Capture mono trichrome HDR | `macapp/hdrctl/mono.go:115` | Ported |  |
| `monopage.go:1146` | PushButton | Stop | `macapp/monoview.go:412` | Ported |  |

### HDR Scan > Focus check

6 items: Ported 6.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `focuspage.go:1006` | TabPage | Focus check | `macapp/focusview.go:94` | Ported |  |
| `focuspage.go:1023` | CheckBox | Center mark | `macapp/focusview.go:67` | Ported |  |
| `focuspage.go:1045` | ComboBox | Zoom | `macapp/focusview.go:107` | Ported |  |
| `focuspage.go:1054` | PushButton | Green only | `macapp/focusview.go:112` | Ported |  |
| `focuspage.go:1056` | PushButton | White | `macapp/focusview.go:114` | Ported |  |
| `focuspage.go:1064` | PushButton | Reset peak | `macapp/focusview.go:139` | Ported |  |

### Crop & Merge > Crop

29 items: Ported 29.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `croppage.go:1192` | MsgBox | Export | `macapp/cropview.go:448` | Ported | in-window dialog |
| `croppage.go:1302` | TabPage | 4. Crop | `macapp/appstate/nav.go:23` | Ported | Crop sub-page |
| `croppage.go:1323` | PushButton | Add files... | `macapp/cropview.go:104` | Ported |  |
| `croppage.go:1331` | PushButton | Add folder... | `macapp/cropview.go:105` | Ported |  |
| `croppage.go:1338` | PushButton | Remove selected | `macapp/cropview.go:106` | Ported |  |
| `croppage.go:1363` | PushButton | Clear | `macapp/cropview.go:107` | Ported |  |
| `croppage.go:1414` | ComboBox | Aspect | `macapp/cropview.go:296` | Ported |  |
| `croppage.go:1422` | ComboBox | Inset | `macapp/cropview.go:300` | Ported |  |
| `croppage.go:1431` | ComboBox | Overlay | `macapp/cropview.go:304` | Ported |  |
| `croppage.go:1438` | CheckBox | Edges | `macapp/cropview.go:308` | Ported |  |
| `croppage.go:1445` | ComboBox | On import | `macapp/cropview.go:317` | Ported |  |
| `croppage.go:1452` | PushButton | Reset to auto | `macapp/cropview.go:322` | Ported |  |
| `croppage.go:1463` | PushButton | Full image | `macapp/cropview.go:323` | Ported |  |
| `croppage.go:1474` | PushButton | Re-detect | `macapp/cropview.go:324` | Ported |  |
| `croppage.go:1487` | PushButton | Apply crop settings to all | `macapp/cropview.go:328` | Ported |  |
| `croppage.go:1512` | LineEdit | Angle | `macapp/cropview.go:60` | Ported |  |
| `croppage.go:1547` | PushButton | -0.5° | `macapp/cropview.go:359` | Ported |  |
| `croppage.go:1548` | PushButton | -0.05° | `macapp/cropview.go:359` | Ported |  |
| `croppage.go:1549` | PushButton | 0 | `macapp/cropview.go:359` | Ported |  |
| `croppage.go:1550` | PushButton | +0.05° | `macapp/cropview.go:359` | Ported |  |
| `croppage.go:1551` | PushButton | +0.5° | `macapp/cropview.go:359` | Ported |  |
| `croppage.go:1562` | ComboBox | Save to | `macapp/cropview.go:454` | Ported |  |
| `croppage.go:1572` | LineEdit | Folder | `macapp/cropview.go:457` | Ported |  |
| `croppage.go:1576` | PushButton | Choose... | `macapp/cropview.go:471` | Ported |  |
| `croppage.go:1588` | ComboBox | TIFF | `macapp/cropview.go:477` | Ported |  |
| `croppage.go:1593` | ComboBox | JPEG quality | `macapp/cropview.go:480` | Ported |  |
| `croppage.go:1600` | ComboBox | Resampling | `macapp/cropview.go:483` | Ported |  |
| `croppage.go:1607` | PushButton | Export selected | `macapp/cropview.go:489` | Ported |  |
| `croppage.go:1608` | PushButton | Export all | `macapp/cropview.go:490` | Ported |  |

### Crop & Merge > Remove hairs

13 items: Ported 13.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `hairpage.go:78` | MsgBox | Remove hairs | `macapp/hdrctl/hair.go:47` | Ported | in-window dialog |
| `hairpage.go:89` | MsgBox | Remove hairs | `macapp/hdrctl/hair.go:47` | Ported | in-window dialog |
| `hairpage.go:142` | MsgBox | Remove hairs | `macapp/hdrctl/hair.go:47` | Ported | in-window dialog |
| `hairpage.go:359` | MsgBox | Remove hairs | `macapp/hdrctl/hair.go:47` | Ported | in-window dialog |
| `hairpage.go:404` | TabPage | Remove hairs | `macapp/hdrctl/hair.go:47` | Ported |  |
| `hairpage.go:411` | LineEdit | TIFF | `macapp/hairsview.go:62` | Ported |  |
| `hairpage.go:416` | PushButton | Browse... | `macapp/hairsview.go:71` | Ported |  |
| `hairpage.go:417` | PushButton | Last HDR | `macapp/hairsview.go:72` | Ported |  |
| `hairpage.go:420` | ComboBox |  | `macapp/hairsview.go:80` | Ported | mode combo (remove hair / zoom) |
| `hairpage.go:421` | PushButton | Fit | `macapp/hairsview.go:83` | Ported |  |
| `hairpage.go:422` | CheckBox | Show mask | `macapp/hairsview.go:85` | Ported |  |
| `hairpage.go:423` | PushButton | Undo | `macapp/hairsview.go:91` | Ported |  |
| `hairpage.go:425` | PushButton | Save into _HDR.tiff | `macapp/hairsview.go:93` | Ported |  |

### Crop & Merge > Batch RAW to TIFF

19 items: Ported 18, Different 1.

| Windows | Kind | Text or label | Mac | Status | Note |
| --- | --- | --- | --- | --- | --- |
| `rawpage.go:422` | MsgBox | Watch folder | `macapp/cropctl/raw.go:525` | Ported | in-window dialog |
| `rawpage.go:426` | MsgBox | Watch folder | `macapp/cropctl/raw.go:525` | Ported | in-window dialog |
| `rawpage.go:563` | TabPage | 3. RAW to TIFF | `macapp/appstate/nav.go:23` | Ported | Batch RAW to TIFF sub-page |
| `rawpage.go:570` | PushButton | Add files... | `macapp/batchview.go:52` | Ported |  |
| `rawpage.go:578` | PushButton | Add folder... | `macapp/batchview.go:53` | Ported |  |
| `rawpage.go:585` | PushButton | Remove selected | `macapp/batchview.go:54` | Ported |  |
| `rawpage.go:602` | PushButton | Clear finished | `macapp/batchview.go:55` | Ported |  |
| `rawpage.go:615` | PushButton | Clear | `macapp/batchview.go:56` | Ported |  |
| `rawpage.go:656` | ComboBox | Save TIFF to | `macapp/batchview.go:97` | Ported |  |
| `rawpage.go:658` | ComboBox | Compression | `macapp/batchview.go:98` | Ported |  |
| `rawpage.go:660` | ComboBox | Embedded ICC profile | `macapp/batchview.go:99` | Ported |  |
| `rawpage.go:663` | ComboBox | Files in parallel | `macapp/batchview.go:102` | Ported | Files in parallel |
| `rawpage.go:667` | CheckBox | Decode through it | `macapp/batchview.go:109` | Different | Adobe DNG Converter: rawconv only knows the Windows install path, so the check box stays off and the label reads "Not available on macOS"... |
| `rawpage.go:681` | LineEdit |  | `macapp/batchview.go:25` | Ported | watch folder field |
| `rawpage.go:682` | PushButton | Choose... | `macapp/batchview.go:137` | Ported |  |
| `rawpage.go:699` | PushButton | Start watching | `macapp/cropctl/raw.go:40` | Ported |  |
| `rawpage.go:718` | PushButton | Convert to TIFF | `macapp/batchview.go:164` | Ported |  |
| `rawpage.go:733` | PushButton | Stop | `macapp/batchview.go:165` | Ported |  |
| `rawpage.go:734` | PushButton | Retry failed | `macapp/batchview.go:166` | Ported |  |

## Tooltips

83 Windows tooltip literals: Ported 74, Ported here 5, Different 4. Rows other than plain Ported:

| Windows | Tooltip | Mac | Status | Note |
| --- | --- | --- | --- | --- |
| `croppage.go:1613` | Same bit depth, ICC profile and metadata kept. Existing files are never overwritten. | `macapp/tips.go:15` | Ported here |  |
| `hdrpage.go:2118` | Send the saved after-profile leveling base when the metadata option is on. Otherwise me... | - | Different | tooltip of the removed Send base to Develop |
| `hdrpage.go:2160` | Captures go into <Save to>\<roll name>\ (created if missing; an existing folder is cont... | `macapp/hdrview.go:522` | Different | same text with / instead of \ in the folder pattern |
| `hdrpage.go:2193` | [roll_]YYYYMMDD_HHMMSS_NNNN_HDR.tiff (time of frame 0; NNNN counts up in the folder), R... | `macapp/tips.go:9` | Ported here |  |
| `hdrpage.go:2268` | Open the merged HDR TIFF in Develop for preview. | - | Different | tooltip of the removed Open in Develop |
| `lightpage.go:1181` | Composite of the three single-channel captures; the target level is shown as mid grey. | `macapp/hdrctl/light_level.go:488` | Ported here |  |
| `monopage.go:1154` | Linear 16-bit TIFF, black level subtracted, nss.scan/1 metadata. CA, HDR, settle and R/... | `macapp/tips.go:13` | Ported here |  |
| `tripage.go:752` | Film base R/G/B measured in the last RGB TIFF -> Develop > Density In for preview. TIFF... | - | Different | tooltip of the removed Send base to Develop |
| `tripage.go:760` | RAFs <name>_R_0EV.RAF ... (same folder, roll and counter as HDR scan). | `macapp/tips.go:11` | Ported here |  |

All other tooltips are on the Mac controls with the Windows text (the hover `Tip` in `macapp/theme.go`).
