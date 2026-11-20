package main

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestClassicRingArtwork(t *testing.T) {
	svg, err := pwaAssets.ReadFile("pwa/icons/agent.svg")
	if err != nil {
		t.Fatal(err)
	}
	for _, geometry := range []string{`viewBox="0 0 64 64"`, `rx="15"`, `fill="#b4d6a3"`, `translate(5 5) scale(.84375)`, `cx="32" cy="32" r="19"`, `stroke="#18211a" stroke-width="4"`, `fill="none"`} {
		if !strings.Contains(string(svg), geometry) {
			t.Errorf("approved Classic ring geometry missing %s", geometry)
		}
	}
	offline, err := pwaAssets.ReadFile("pwa/offline.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(offline), strings.TrimSpace(string(svg))) || strings.Contains(string(offline), ">e1<") {
		t.Error("offline page must embed the shared Classic ring without a network dependency")
	}
	for _, path := range []string{"pwa/icons/icon-192.png", "pwa/icons/icon-512.png", "pwa/icons/apple-touch-icon.png"} {
		data, err := pwaAssets.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		size := img.Bounds().Dx()
		// Center is empty sage; the ring crosses 25% of the width, not the center.
		for _, pixel := range []struct {
			x, y int
			rgb  [3]uint32
		}{
			{size / 2, size / 2, [3]uint32{0xb4, 0xd6, 0xa3}},
			{size / 4, size / 2, [3]uint32{0x18, 0x21, 0x1a}},
			{size / 2, size / 10, [3]uint32{0xb4, 0xd6, 0xa3}},
			{0, 0, [3]uint32{0x18, 0x1b, 0x1a}},
		} {
			r, g, b, _ := img.At(pixel.x, pixel.y).RGBA()
			if got := [3]uint32{r >> 8, g >> 8, b >> 8}; got != pixel.rgb {
				t.Errorf("%s pixel (%d,%d) = %v, want %v", path, pixel.x, pixel.y, got, pixel.rgb)
			}
		}
	}
}
