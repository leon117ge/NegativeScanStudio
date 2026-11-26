NegativeScan Studio OFX for macOS (Apple Silicon)
=================================================

What is in this folder
- NegativeScanStudio.ofx.bundle: the OFX plugin (arm64, CPU render path, ad hoc signed).
- Install.command: installs the plugin for DaVinci Resolve and other OFX hosts.

Install
1. Unzip the download.
2. Right-click Install.command and choose Open (the first time macOS asks because the
   script is not from the App Store). Or open Terminal and run:  bash Install.command
3. Enter your administrator password when asked.
4. Restart DaVinci Resolve.

The installer copies the plugin to /Library/OFX/Plugins, clears the download quarantine
flag (without this, Resolve skips the plugin silently) and signs it ad hoc for this Mac.

Notes
- Apple Silicon Macs only (M1 and later), macOS 12 or later. If Resolve runs under
  Rosetta (Intel mode), it cannot load this plugin.
- Render engine: CPU, using every core macOS reports. CUDA and OpenCL are not used on
  macOS. Output matches the Windows CPU render within float rounding.
- To remove the plugin: sudo rm -rf /Library/OFX/Plugins/NegativeScanStudio.ofx.bundle
- If the plugin does not appear, check the Resolve log:
  ~/Library/Application Support/Blackmagic Design/DaVinci Resolve/logs/ResolveDebug.txt
