package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestPWABuildMetadataRoutes(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	original := version
	t.Cleanup(func() { version = original })
	var previous string
	for _, value := range []string{"pwa-test-A", "pwa-test-B"} {
		version = value
		server := newServer()
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest("GET", "/version.json", nil))
		if rec.Code != 200 {
			t.Fatalf("version.json status = %d, want 200", rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("version headers = %v", rec.Header())
		}
		var metadata map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &metadata); err != nil {
			t.Fatal(err)
		}
		// Independently fingerprint each input. This catches omitted HTML, update JS,
		// manifest, worker, offline page, icon bytes, or release version.
		parts := [][]byte{[]byte(version), []byte(homePage), []byte(pwaUpdateScript), []byte(pwaManifest)}
		if err := fs.WalkDir(pwaAssets, "pwa", func(path string, entry fs.DirEntry, err error) error {
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
		}); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(parts)
		if err != nil {
			t.Fatal(err)
		}
		expected := fmt.Sprintf("%x", sha256.Sum256(encoded))
		if metadata["buildId"] != expected {
			t.Errorf("buildId does not include every deployed input: got %s want %s", metadata["buildId"], expected)
		}
		if metadata["version"] != value || len(metadata["buildId"]) != 64 || metadata["buildId"] == previous {
			t.Errorf("metadata = %v, previous %s", metadata, previous)
		}
		previous = metadata["buildId"]
		for _, path := range []string{"/", "/sw.js", "/pwa-updates.js", "/version.json"} {
			get := httptest.NewRecorder()
			server.ServeHTTP(get, httptest.NewRequest("GET", path, nil))
			if get.Code != 200 || strings.Contains(get.Body.String(), "{{APP_") {
				t.Errorf("%s status/body not rendered", path)
			}
			if path == "/" {
				if get.Header().Get("Cache-Control") != "no-store" {
					t.Error("root must not be cached")
				}
				if !strings.Contains(get.Body.String(), previous) || !strings.Contains(get.Body.String(), value) {
					t.Error("root missing build/version")
				}
			} else if get.Header().Get("Cache-Control") != "no-cache" {
				t.Errorf("%s must revalidate", path)
			}
			if path == "/pwa-updates.js" && get.Body.String() != pwaUpdateScript {
				t.Error("wrong update script")
			}
			if path == "/sw.js" && !strings.Contains(get.Body.String(), previous) {
				t.Error("worker missing content build ID")
			}
			head := httptest.NewRecorder()
			server.ServeHTTP(head, httptest.NewRequest("HEAD", path, nil))
			if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Cache-Control") != get.Header().Get("Cache-Control") {
				t.Errorf("HEAD %s incorrect: %d %v %d", path, head.Code, head.Header(), head.Body.Len())
			}
		}
	}
}

func TestPWAWorkerPrivacyLifecycle(t *testing.T) {
	body, err := pwaAssets.ReadFile("pwa/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"install", "activate", "fetch", "SKIP_WAITING", "clients.claim"} {
		if !bytes.Contains(body, []byte(required)) {
			t.Fatalf("worker missing %s behavior", required)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable; worker source safety assertions passed")
	}
	cmd := exec.Command(node, "-e", workerPrivacyHarness)
	cmd.Stdin = bytes.NewReader(body)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worker privacy lifecycle: %v\n%s", err, out)
	}
}

const workerPrivacyHarness = `
const vm = require('node:vm'), assert = require('node:assert/strict');
const source = require('node:fs').readFileSync(0, 'utf8');
const handlers = {}, entries = new Map(), opened = [], deleted = [], requests = [];
let skipped = 0, claimed = 0, offline = false;
const page = {ok:true, redirected:false, clone(){return this;}};
const cache = {async put(key,value){entries.set(key,value)}, async match(key){return entries.get(key)}};
const context = { URL, self: {location: {origin:'https://agent.test'}, addEventListener(name, fn){handlers[name]=fn}, skipWaiting(){skipped++;return Promise.resolve()}, clients:{claim(){claimed++;return Promise.resolve()}}},
 caches:{async open(name){opened.push(name);return cache},async keys(){return ['agent-pwa-offline-old','finance-offline-v1',...opened]}, async delete(name){deleted.push(name)}},
 async fetch(request, options){requests.push([request,options]);if(offline)throw Error('offline');return page;}
};
vm.runInNewContext(source, context);
async function lifecycle(name){let work;handlers[name]({waitUntil(p){work=p}});await work;}
function dispatch(request){let result;handlers.fetch({request, respondWith(p){result=p}});return result;}
(async()=>{
 await lifecycle('install');
 assert.equal(skipped,0,'install must not replace active worker');
 assert.deepEqual([...entries.keys()],['/offline.html'],'only public offline page cached');
 assert.equal(requests.length,1);assert.equal(requests[0][0],'/offline.html');
 assert.equal(requests[0][1].credentials,'omit');assert.equal(requests[0][1].cache,'no-store');
 await lifecycle('activate');assert.equal(claimed,1);
 assert.deepEqual(deleted,['agent-pwa-offline-old'],'do not remove other applications/current cache');
 handlers.message({data:{type:'NOT_SKIP_WAITING'}});handlers.message({data:null});assert.equal(skipped,0);
 handlers.message({data:{type:'SKIP_WAITING'}});assert.equal(skipped,1);
 const request = (path, more={})=>({url:new URL(path,'https://agent.test').href,method:'GET',mode:'navigate',destination:'document',...more});
 assert.equal(await dispatch(request('/')),page,'online document from network');
 offline=true;
 assert.equal(await dispatch(request('/?private=not-cached')),page,'offline public fallback');
 for(const path of ['/api','/api/chat','/api/auth/me','/auth','/auth/callback','/version','/version.json','/healthz']) {
  assert.equal(dispatch(request(path)),undefined,'excluded navigation '+path);
 }
 for(const req of [request('https://other.test/'),request('/',{method:'POST'}),request('/api/chat',{method:'POST',mode:'cors',destination:''}),request('/',{mode:'cors',destination:''}),request('/pwa-updates.js',{mode:'no-cors',destination:'script'})]) {
  assert.equal(dispatch(req),undefined,'network only '+req.url);
 }
 assert.deepEqual([...entries.keys()],['/offline.html'],'never cache private data or shell');
 assert.equal(opened.every(name=>name.startsWith('agent-pwa-offline-')),true);
})().catch(error=>{console.error(error);process.exitCode=1});
`

func TestPWAPublicAssets(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	server := newServer()
	for _, asset := range []struct {
		path, contentType string
		size              int
	}{
		{"/offline.html", "text/html; charset=utf-8", 0},
		{"/sw.js", "text/javascript; charset=utf-8", 0},
		{"/icons/icon-192.png", "image/png", 192},
		{"/icons/icon-512.png", "image/png", 512},
		{"/icons/apple-touch-icon.png", "image/png", 180},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(method, asset.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s = %d, want 200", method, asset.path, rec.Code)
			}
			if rec.Header().Get("Content-Type") != asset.contentType || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s headers = %v", asset.path, rec.Header())
			}
			if method == http.MethodHead {
				if rec.Body.Len() != 0 {
					t.Error("HEAD body")
				}
				continue
			}
			if asset.size != 0 {
				img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds().Dx() != asset.size || img.Bounds().Dy() != asset.size {
					t.Errorf("%s dimensions = %v", asset.path, img.Bounds())
				}
			} else if rec.Body.Len() == 0 {
				t.Errorf("empty %s", asset.path)
			}
		}
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/offline.html", nil))
	offline := rec.Body.String()
	if !strings.Contains(offline, "Agent") || !strings.Contains(offline, "offline") || strings.Contains(offline, "<script") || strings.Contains(offline, "{{") {
		t.Errorf("offline page must be a public static disconnected page: %s", offline)
	}
}

func TestPWAManifestPublicRoutes(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	server := newServer()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(method, "/manifest.webmanifest", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s manifest status = %d, want 200", method, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "no-cache") {
			t.Errorf("manifest must revalidate: %v", rec.Header())
		}
		if rec.Header().Get("Content-Type") != "application/manifest+json; charset=utf-8" {
			t.Errorf("manifest content type = %q", rec.Header().Get("Content-Type"))
		}
		if method == http.MethodHead {
			if rec.Body.Len() != 0 {
				t.Error("HEAD returned body")
			}
			continue
		}
		var manifest struct {
			Name, Display, StartURL, Scope string
			Icons                          []struct{ Src, Sizes, Type string }
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Name != "Agent" || manifest.Display != "standalone" {
			t.Errorf("manifest = %+v", manifest)
		}
		if string(raw["start_url"]) != `"/"` || string(raw["scope"]) != `"/"` || len(manifest.Icons) < 2 {
			t.Errorf("incomplete manifest: %s", rec.Body.String())
		}
	}
}
