# Historical 500T DCTL workflow through v0.90

This is an approximate model derived from the Kodak data sheet, not a measured white balance for this roll.
If an 85 filter was used when shooting, or the light source was mixed, LED or fluorescent, do not apply the unfiltered daylight preset directly.

1. Finish film base leveling in Camera \u0026 Light \u003e Light, and write the film base RGB into the DCTL with the original button.
2. Keep the light unchanged. Expand **Colour cast** on the left.
3. Pick the light source the negative was shot under. For unfiltered daylight, first pick **500T @ 5500K**.
   For tungsten, pick **3200K**; its offset is zero. The custom range is 2500–10000K.
4. Check **Enable**, click **Calculate**, and review the signed density offsets.
   At 5500K they are about R +0.0926, G 0, B -0.1886 D.
5. Click **Write DCTL** in the colour cast section. The program saves the current export settings and does not send any light commands.
6. Export Density_In on the DCTL page. If Auto-sync is on, the existing export path is updated.
7. Reload the DCTL in Resolve, and reset the node or check `cast_on/cast_r/g/b`.
   Slider values kept by an existing node do not change automatically when the file is updated.
8. Save it as `500T @ 5500K` with **Save stock preset**. Later, pick the saved stock in the colour cast section,
   click **Load stock**, then click **Write DCTL** to activate it. Loading does not overwrite the current film base or change the light.

To see the effect inside the program, click **Send to Develop**. This button changes only the Density In
colour cast parameters of the current Develop, and does not reset the existing Positive Out gamma, gray point or auto white balance.
If the old auto white balance already removed the same cast, turn it off first to avoid compensating twice.

To disable it, uncheck **Enable** and click **Write DCTL**. The export settings are an independent global state;
switching to another film stock or only switching the film base preset does not turn off the 500T correction automatically.

Measured gray points and gray-scale cross cast fitting are not in v0.89. Software tests and cross-compilation are done,
but it has not been accepted on a real camera, light and Resolve.


This workflow is removed from app v0.91. See [current preview workflow](500T_USAGE.md).
