//go:build windows

package main

import (
	"math"
	"unsafe"
)

var pGetSystemMetrics = user32.NewProc("GetSystemMetrics")

// batteryIcon draws a horizontal battery: fill proportional to percent,
// coloured by level; a lightning bolt when charging; grey outline if unknown.
// percent < 0 means "no data".
func batteryIcon(percent int, charging, full bool) uintptr {
	n, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	S := int(n)
	if S < 16 {
		S = 16
	}
	cv := newCanvas(S, S)
	if cv == nil {
		return 0
	}
	defer cv.free()
	cv.fill(0)
	cv.TransparentBase = true
	alpha := make([]float64, S*S)
	f := float64(S) / 16.0

	white := uint32(0xFFFFFF)
	grey := uint32(0x9E9E9E)
	outline := white
	if percent < 0 {
		outline = grey
	}
	// body outline: outer rect minus inner rect
	bx0, by0, bx1, by1 := 0.5*f, 3.5*f, 13.5*f, 12.5*f
	t := math.Max(1.0, 1.25*f)
	rr := 1.8 * f
	cv.blendShape(0, 0, S, S, outline, alpha, func(px, py float64) float64 {
		outer := roundRectCoverage(px, py, bx0, by0, bx1, by1, rr)
		inner := roundRectCoverage(px, py, bx0+t, by0+t, bx1-t, by1-t, math.Max(rr-t, 0))
		return outer * (1 - inner)
	})
	// terminal nub
	cv.blendShape(0, 0, S, S, outline, alpha, func(px, py float64) float64 {
		return roundRectCoverage(px, py, 13.5*f-0.2, 6*f, 15.5*f, 10*f, 0.6*f)
	})
	if percent >= 0 {
		p := clampF(float64(percent)/100.0, 0, 1)
		col := uint32(0x43A047) // green
		switch {
		case percent <= 15:
			col = 0xE53935
		case percent <= 40:
			col = 0xFB8C00
		}
		if charging || full {
			col = 0x43A047
		}
		ix0, iy0, ix1, iy1 := bx0+t+0.8*f, by0+t+0.8*f, bx1-t-0.8*f, by1-t-0.8*f
		w := (ix1 - ix0) * p
		if p > 0 && w < 1.2 {
			w = 1.2
		}
		cv.blendShape(0, 0, S, S, col, alpha, func(px, py float64) float64 {
			return roundRectCoverage(px, py, ix0, iy0, ix0+w, iy1, 0.6*f)
		})
	}
	if charging {
		// lightning bolt as two triangles
		bolt := [][3][2]float64{
			{{8.3, 3.0}, {4.8, 8.6}, {8.0, 8.6}},
			{{7.4, 13.0}, {11.0, 7.2}, {7.8, 7.2}},
		}
		for _, tri := range bolt {
			tr := tri
			for i := range tr {
				tr[i][0] *= f
				tr[i][1] *= f
			}
			cv.blendShape(0, 0, S, S, 0xFFEB3B, alpha, func(px, py float64) float64 {
				return triCoverage(px, py, tr)
			})
		}
	}

	// non-premultiplied alpha for icons
	for i := range cv.Pix {
		a := uint32(clampF(alpha[i], 0, 1)*255 + 0.5)
		cv.Pix[i] = a<<24 | (cv.Pix[i] & 0xFFFFFF)
	}
	mask, _, _ := pCreateBitmap.Call(uintptr(S), uintptr(S), 1, 1, 0)
	defer pDeleteObject.Call(mask)
	// detach the colour bitmap from its DC before handing it to CreateIconIndirect
	pSelectObject.Call(cv.DC, cv.old)
	ii := ICONINFO{FIcon: 1, HbmMask: mask, HbmColor: cv.BMP}
	h, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	pSelectObject.Call(cv.DC, cv.BMP) // so free() restores consistently
	return h
}

// triCoverage: 4x4 supersampled triangle coverage.
func triCoverage(px, py float64, t [3][2]float64) float64 {
	in := 0
	for sy := 0; sy < 4; sy++ {
		for sx := 0; sx < 4; sx++ {
			x := px - 0.5 + (float64(sx)+0.5)/4
			y := py - 0.5 + (float64(sy)+0.5)/4
			if pointInTri(x, y, t) {
				in++
			}
		}
	}
	return float64(in) / 16
}

func pointInTri(x, y float64, t [3][2]float64) bool {
	s := func(a, b [2]float64) float64 { return (x-b[0])*(a[1]-b[1]) - (a[0]-b[0])*(y-b[1]) }
	d1, d2, d3 := s(t[0], t[1]), s(t[1], t[2]), s(t[2], t[0])
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}
