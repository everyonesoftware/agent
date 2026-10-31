package main

import (
	"fmt"
	"net/http"
)

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, version)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = fmt.Fprint(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>EveryoneAgent</title>
  <style>
    :root { color-scheme: light; font: 16px/1.55 system-ui, sans-serif; color: #29252b; background: #f7f5f2; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; }
    main { box-sizing: border-box; width: min(42rem, calc(100% - 2rem)); padding: 2.5rem; background: white; border: 1px solid #e8e3de; border-radius: 1.25rem; box-shadow: 0 1rem 3rem #30241b0c; }
    .mark { color: #7355a5; font-weight: 700; letter-spacing: .04em; }
    h1 { margin: .6rem 0; font-size: clamp(2rem, 6vw, 3rem); letter-spacing: -.04em; }
    p { color: #625c63; }
    .status { display: inline-flex; align-items: center; gap: .5rem; padding: .4rem .75rem; border-radius: 99px; background: #f1ebf8; color: #64468f; font-size: .9rem; }
    .dot { width: .55rem; height: .55rem; border-radius: 50%; background: #7755a7; }
  </style>
</head>
<body>
  <main>
    <div class="mark">EVERYONEAGENT</div>
    <h1>Your private AI workspace</h1>
    <p>The app foundation is running. Chat, accounts, private conversation storage, and web search are the next implementation steps.</p>
    <span class="status"><span class="dot"></span>Private app foundation ready</span>
  </main>
</body>
</html>`)
	})
	return mux
}
