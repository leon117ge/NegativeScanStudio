package rawconv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultDNGConverter is the standard install location on Windows.
const DefaultDNGConverter = `C:\Program Files\Adobe\Adobe DNG Converter\Adobe DNG Converter.exe`

// DNGConverterPath returns the converter path if it is installed, else "".
func DNGConverterPath() string {
	if st, err := os.Stat(DefaultDNGConverter); err == nil && !st.IsDir() {
		return DefaultDNGConverter
	}
	return ""
}

func runDNG(ctx context.Context, exe string, args []string) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg != "" {
			return fmt.Errorf("%v: %s", err, msg)
		}
		return err
	}
	return nil
}

// decodeViaDNG reproduces open_make_tiff's decodeDNG: RAW -> uninterpolated
// DNG (CR 5.4) -> linear DNG 1.1 -> LibRaw with raw inset crop.
func decodeViaDNG(ctx context.Context, exe, src, dir, name, tok string) (*Image, error) {
	pre := fmt.Sprintf("%s_%s.int_pre.dng", name, tok)
	lin := fmt.Sprintf("%s_%s.int.dng", name, tok)
	prePath := filepath.Join(dir, pre)
	linPath := filepath.Join(dir, lin)
	defer os.Remove(prePath)
	defer os.Remove(linPath)

	if err := runDNG(ctx, exe, []string{"-u", "-p0", "-d", dir, "-o", pre, "-cr5.4", src}); err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}
	if _, err := os.Stat(prePath); err != nil {
		return nil, fmt.Errorf("pass 1 produced no DNG")
	}
	if err := runDNG(ctx, exe, []string{"-u", "-l", "-p0", "-d", dir, "-o", lin, "-dng1.1", prePath}); err != nil {
		return nil, fmt.Errorf("pass 2: %w", err)
	}
	if _, err := os.Stat(linPath); err != nil {
		return nil, fmt.Errorf("pass 2 produced no DNG")
	}
	return decode(linPath, true)
}
