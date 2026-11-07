# Preview a 500T illuminant correction

This app workflow affects only the built-in Develop preview. For Resolve, use the independent [OFX plugin](../ofx/README.md). The app no longer generates DCTL files.

1. Open a scan in **Develop**.
2. Open **Camera & Light > Light**.
3. In **500T illuminant — Develop preview**, select the original scene illuminant or enter a temperature from 2500 to 10000 K.
4. Select **Enable**.
5. Click **Calculate** to inspect the additive density offsets.
6. Click **Send to Develop** to apply those offsets to the **Density In** node.
7. Return to **Develop** to inspect the image and adjust the existing controls.

To disable the correction, clear **Enable** and click **Send to Develop** again. The calculator sends no camera or light commands and does not change the film-base sliders.

The model uses nominal Kodak 5219 sensitivity, a Planck illuminant, and approximate ECN-2 slopes. It is not a measured white balance. Existing creative gamma and auto-WB settings stay unchanged, so check for double correction.

The old saved-stock/export workflow is preserved as [historical documentation](500T_USAGE_LEGACY.md), not current instructions. Old settings and film-database entries are not deleted.
