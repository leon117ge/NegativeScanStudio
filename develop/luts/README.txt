Embedded print-film LUTs of the Develop module, node 8 "Cineon -> 2383".

The program embeds every *.cube file in this folder (go:embed) and uses
  Kodak_2383_D55.cube, Kodak_2383_D60.cube, Kodak_2383_D65.cube
as the default LUT source. Input: Cineon Film Log, output Rec.709 gamma 2.4.

Since v0.70 these are the real Resolve Film Look LUTs supplied by the user
(Kodak2383_D55/D60/D65.cube, LUT_3D_SIZE 33, LUT_3D_INPUT_RANGE 0-1).
A file whose header has "# FBM_PLACEHOLDER" is reported as a placeholder.
To change them: copy the new .cube over these names and rebuild the exe.
