package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed pwa
var pwaAssets embed.FS

const pwaManifest = `{"id":"/","name":"Agent","short_name":"Agent","start_url":"/","scope":"/","display":"standalone","background_color":"#202322","theme_color":"#202322","icons":[{"src":"/icons/icon-192.png","sizes":"192x192","type":"image/png","purpose":"any"},{"src":"/icons/icon-512.png","sizes":"512x512","type":"image/png","purpose":"any"}]}`

// appBuildID fingerprints source templates, not their token-expanded output, to
// avoid a circular worker hash. WalkDir gives deterministic asset ordering.
func appBuildID() string {
	// JSON encodes byte slices as base64, preserving arbitrary PNG bytes rather
	// than replacing non-UTF-8 bytes as it would for strings.
	parts := [][]byte{[]byte(version), []byte(homePage), []byte(pwaUpdateScript), []byte(pwaManifest)}
	err := fs.WalkDir(pwaAssets, "pwa", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := pwaAssets.ReadFile(path)
		if err != nil {
			return err
		}
		parts = append(parts, []byte(path), data)
		return nil
	})
	if err != nil {
		panic(err)
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func registerPWARoutes(mux *http.ServeMux) {
	buildID := appBuildID()
	serve := func(path, contentType string, body []byte) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method != http.MethodHead {
				_, _ = w.Write(body)
			}
		})
	}
	serve("/manifest.webmanifest", "application/manifest+json; charset=utf-8", []byte(pwaManifest))
	serve("/pwa-updates.js", "text/javascript; charset=utf-8", []byte(pwaUpdateScript))
	metadata, err := json.Marshal(struct {
		Version string `json:"version"`
		BuildID string `json:"buildId"`
	}{version, buildID})
	if err != nil {
		panic(err)
	}
	serve("/version.json", "application/json; charset=utf-8", metadata)
	for path, contentType := range map[string]string{
		"/offline.html":               "text/html; charset=utf-8",
		"/sw.js":                      "text/javascript; charset=utf-8",
		"/icons/icon-192.png":         "image/png",
		"/icons/icon-512.png":         "image/png",
		"/icons/apple-touch-icon.png": "image/png",
	} {
		body, err := pwaAssets.ReadFile("pwa" + path)
		if err != nil {
			panic(err)
		}
		if path == "/sw.js" {
			body = []byte(strings.ReplaceAll(string(body), "{{APP_BUILD_ID}}", buildID))
		}
		serve(path, contentType, body)
	}
}
