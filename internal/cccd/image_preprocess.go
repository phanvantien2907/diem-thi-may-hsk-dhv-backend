package cccd

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"

	"github.com/hskvinh/backend/internal/cccd/entities"
)

// PrepareImage validates image size, resolution, sharpness, and brightness in memory,
// downscales if the longest edge exceeds cfg.MaxImageEdge, and encodes as JPEG (quality 90).
func PrepareImage(raw []byte, cfg Config) (entities.PreparedImage, error) {
	if len(raw) == 0 {
		return entities.PreparedImage{}, NewPipelineError(ErrCodeInvalidParams, "Thiếu file ảnh trong request")
	}
	if cfg.MaxUploadBytes > 0 && len(raw) > cfg.MaxUploadBytes {
		mb := float64(cfg.MaxUploadBytes) / (1024.0 * 1024.0)
		return entities.PreparedImage{}, NewPipelineError(ErrCodeFileTooLarge, fmt.Sprintf("Ảnh vượt quá %g MB", mb))
	}

	// Quick config check to guard against decompression bombs before full decode
	imgCfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(raw))
	if cfgErr != nil {
		if errors.Is(cfgErr, image.ErrFormat) {
			return entities.PreparedImage{}, NewPipelineError(ErrCodeInvalidImage, "File gửi lên không phải là file ảnh", cfgErr)
		}
		return entities.PreparedImage{}, NewPipelineError(ErrCodeBadImageData, "File ảnh bị hỏng hoặc định dạng không được hỗ trợ", cfgErr)
	}
	if int64(imgCfg.Width)*int64(imgCfg.Height) > 40_000_000 {
		return entities.PreparedImage{}, NewPipelineError(ErrCodeBadImageData, "File ảnh bị hỏng hoặc định dạng không được hỗ trợ")
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return entities.PreparedImage{}, NewPipelineError(ErrCodeInvalidImage, "File gửi lên không phải là file ảnh", err)
		}
		return entities.PreparedImage{}, NewPipelineError(ErrCodeBadImageData, "File ảnh bị hỏng hoặc định dạng không được hỗ trợ", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= 0 || h <= 0 {
		return entities.PreparedImage{}, NewPipelineError(
			ErrCodeBadImageData,
			"File ảnh không có kích thước hợp lệ",
		)
	}

	warnings := computeQualityWarnings(img, cfg)

	longest := w
	if h > longest {
		longest = h
	}
	if cfg.MaxImageEdge > 0 && longest > cfg.MaxImageEdge {
		scale := float64(cfg.MaxImageEdge) / float64(longest)
		newW := int(math.Round(float64(w) * scale))
		newH := int(math.Round(float64(h) * scale))
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}
		img = resizeBilinear(img, newW, newH)
		w = newW
		h = newH
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return entities.PreparedImage{}, NewPipelineError(ErrCodeBadImageData, "Không thể chuẩn hóa định dạng ảnh", err)
	}

	return entities.PreparedImage{
		Data:     buf.Bytes(),
		Mime:     "image/jpeg",
		Width:    w,
		Height:   h,
		Warnings: warnings,
	}, nil
}

func computeQualityWarnings(img image.Image, cfg Config) []string {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Downsample to at most 800x800 for fast grayscale edge & brightness statistics
	thumbW, thumbH := w, h
	maxThumb := 800
	if w > maxThumb || h > maxThumb {
		if w >= h {
			thumbW = maxThumb
			thumbH = int(math.Max(1, math.Round(float64(h)*float64(maxThumb)/float64(w))))
		} else {
			thumbH = maxThumb
			thumbW = int(math.Max(1, math.Round(float64(w)*float64(maxThumb)/float64(h))))
		}
	}

	gray := make([]float64, thumbW*thumbH)
	var sumLuma float64
	for y := 0; y < thumbH; y++ {
		srcY := bounds.Min.Y + (y*h)/thumbH
		for x := 0; x < thumbW; x++ {
			srcX := bounds.Min.X + (x*w)/thumbW
			r, g, b, _ := img.At(srcX, srcY).RGBA()
			// ITU-R BT.601 luma in 0..255 range
			luma := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			gray[y*thumbW+x] = luma
			sumLuma += luma
		}
	}

	totalPixels := float64(thumbW * thumbH)
	brightnessMean := sumLuma / totalPixels

	// Compute 3x3 Laplacian/edge magnitude variance (analogous to Pillow FIND_EDGES variance)
	var edgeSum, edgeSqSum float64
	var edgeCount int
	for y := 1; y < thumbH-1; y++ {
		for x := 1; x < thumbW-1; x++ {
			c := gray[y*thumbW+x]
			lap := math.Abs(
				8*c -
					gray[(y-1)*thumbW+(x-1)] - gray[(y-1)*thumbW+x] - gray[(y-1)*thumbW+(x+1)] -
					gray[y*thumbW+(x-1)] - gray[y*thumbW+(x+1)] -
					gray[(y+1)*thumbW+(x-1)] - gray[(y+1)*thumbW+x] - gray[(y+1)*thumbW+(x+1)],
			)
			if lap > 255 {
				lap = 255
			}
			edgeSum += lap
			edgeSqSum += lap * lap
			edgeCount++
		}
	}

	var sharpnessVar float64
	if edgeCount > 0 {
		meanEdge := edgeSum / float64(edgeCount)
		sharpnessVar = (edgeSqSum / float64(edgeCount)) - (meanEdge * meanEdge)
	}

	warnings := []string{}
	if cfg.MinSharpness > 0 && sharpnessVar < cfg.MinSharpness {
		warnings = append(warnings, "Ảnh có thể bị mờ, nên chụp lại nếu kết quả sai")
	}
	if cfg.MinBrightness > 0 && brightnessMean < cfg.MinBrightness {
		warnings = append(warnings, "Ảnh quá tối")
	} else if cfg.MaxBrightness > 0 && brightnessMean > cfg.MaxBrightness {
		warnings = append(warnings, "Ảnh quá sáng / lóa")
	}
	return warnings
}

func resizeBilinear(src image.Image, dstW, dstH int) *image.RGBA {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)

	for y := 0; y < dstH; y++ {
		sy := (float64(y)+0.5)*yRatio - 0.5
		y0 := int(math.Floor(sy))
		y1 := y0 + 1
		fy := sy - float64(y0)
		if y0 < 0 {
			y0 = 0
			fy = 0
		}
		if y1 >= srcH {
			y1 = srcH - 1
		}

		for x := 0; x < dstW; x++ {
			sx := (float64(x)+0.5)*xRatio - 0.5
			x0 := int(math.Floor(sx))
			x1 := x0 + 1
			fx := sx - float64(x0)
			if x0 < 0 {
				x0 = 0
				fx = 0
			}
			if x1 >= srcW {
				x1 = srcW - 1
			}

			r00, g00, b00, _ := src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y0).RGBA()
			r10, g10, b10, _ := src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y0).RGBA()
			r01, g01, b01, _ := src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y1).RGBA()
			r11, g11, b11, _ := src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y1).RGBA()

			w00 := (1 - fx) * (1 - fy)
			w10 := fx * (1 - fy)
			w01 := (1 - fx) * fy
			w11 := fx * fy

			r := clampByte(float64(r00>>8)*w00 + float64(r10>>8)*w10 + float64(r01>>8)*w01 + float64(r11>>8)*w11)
			g := clampByte(float64(g00>>8)*w00 + float64(g10>>8)*w10 + float64(g01>>8)*w01 + float64(g11>>8)*w11)
			b := clampByte(float64(b00>>8)*w00 + float64(b10>>8)*w10 + float64(b01>>8)*w01 + float64(b11>>8)*w11)

			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return dst
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
