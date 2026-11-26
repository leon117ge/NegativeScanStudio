# Film white balance: visible slider Base loaders (OFX 0.90.3)

Table source: [ofx_film_bases.json](ofx_film_bases.json), which generates `ofx/src/film_bases.hpp`, the dropdown and the defaults. Each stock has **at most one, and only a measured, Base entry**; each supported stock keeps one target slot, and no numbers are invented for unmeasured stocks. Currently there is only **500T Base**; other stocks will get a `\u003cStock\u003e Base` after they are measured, and multiple Bases will not be created per color temperature/gamma. `None` is a reset action, not a stock.

| UI loader | Red balance / stops | Green | Blue |
|---|---:|---:|---:|
| None | 0 | 0 | 0 |
| 500T Base | +1.07 | 0 | +0.67 |

Choosing from the dropdown **immediately writes these three visible sliders** in instanceChanged; they can still be edited by hand. The default dropdown is 500T Base, and the default sliders are likewise 1.07/0/0.67. Render does not read the dropdown to compute an extra gain: the old `ar/ab` preset gain is always zero (the gray point computes its gain separately with the original formula), so nothing is applied twice. Choosing None explicitly resets the three balances to 0; you can fine-tune by hand after choosing. The dropdown means "the last loaded preset", not a lock on the sliders.

Film-base T/D, stock gamma and Base balance are independent visible loaders; switching T/D/stock **no longer silently clears or disables** the balances that are already visible; the two can be stacked explicitly. In 0.90.3 the current Base was set by the user's eye adjustment; the Gamma scale 1.15 and Exposure 0.90 defaults stay unchanged, and they are not a newly fitted scientific measurement. The T5600 table is unchanged. Turning on Auto gray from point is a new neutral correction: it sets the dropdown to None, clears the visible balances to 0, then computes the gray gain per frame from the source gray point; the balance can still be fine-tuned by hand afterwards. Turning off Auto gray does not automatically restore the old Base; reload it when needed.

## 0.90.3 user eye-adjustment decision

The current 500T Base was set by the user to **+1.07/0/+0.67** (previously +1.40/0/+0.80). Only this loader and the visible balance defaults for new instances were updated; no new measurement was made. Existing projects keep the slider values saved by the host; re-selecting 500T Base loads the new values.

## Historical 0.90.2 user eye-adjustment decision

At that time 500T Base changed from the old **+1.57/0/+1.00** to **+1.40/0/+0.80**. The user judged the old values too red under the new defaults and specified these values directly. Conditions were Gamma scale 1.15, Exposure 0.90 and the T5600 background workflow; no further claim was made about the raw gamma or white point of that eye adjustment. The re-measurement was canceled; no new patch measurement/fit was made, and the eye-adjusted numbers are not labeled as official Kodak values.

## Historical 0.90.1 measurement conditions and scope

Results from the user-provided `/workspace/wb0206/residual_measurements.json`; the raw values are stored in [ofx_500t_base_measurements.json](ofx_500t_base_measurements.json):

- 0206.tiff daylight frame, baseline .5/.5/.5, **T5600 enabled**, gamma .45/.54/.53, white code696.
- T5600 density offsets kept at +.094824/0/−.193475; the corresponding effective base is .622006/.5/.320254, not changed further.
- Two gray roof regions R+1.57180/+1.56086, B+.94505/+.97872; white wall/white car about R+1.56696…+1.57164, B+.99378…+1.17346; clouds R+1.67382, B+.87683. At that time the user chose the suggested residual +1.57/0/+1.00; it is now kept as a historical record and is not the current 0.90.3 preset.
- This is an **empirical residual** for this scanning system/frame/workflow, not an official Kodak stock constant or an independent color chart calibration, and it is not guaranteed to apply directly to other lighting, scanners, exposure/processing or stocks. Under other gamma/white point settings it can still be adjusted by hand.
- R/B are constants and are no longer multiplied by the old 0.0013 slope with white_code; the old condition was a scan under a different white light, and the slope cannot be carried over without justification. white_code still affects the white point/inversion through the original Positive formula, but does not change these two loader numbers.

The standalone Go DCTL reference was not modified: the tests set its old hidden WB to None and compare against an equivalent manual balance. This request intentionally changed the plugin's WB values, and makes no claim of matching the original Legacy WB output. Back up old v0.90 projects, and reload Base when needed.
