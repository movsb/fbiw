# Browser example

Build with Go 1.27 or newer:

```sh
GOOS=js GOARCH=wasm go build
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
python3 -m http.server 8765
```

Open <http://localhost:8765/>. The page needs WebGL and must be served over HTTP rather than opened as a `file:` URL. Use W/S to zoom, A/D to change spin direction, K to enlarge and J to reset. The canvas follows the browser viewport. `app.wasm` and `wasm_exec.js` are generated build artifacts.
