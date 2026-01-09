# Dashboard Frontend

⚠️ **DO NOT BUILD THIS DIRECTORY DIRECTLY**

This React frontend is built via the Go toolchain to ensure consistency
and handle dependencies properly.

## Building

From the **project root**, run:
```bash
go run ./cmd/build-dashboard
```

This command will:
1. Install npm dependencies
2. Run the Vite build
3. Output to `./schmux` binary

## What NOT to do

❌ `cd assets/dashboard && npm install`
❌ `cd assets/dashboard && npm run build`
❌ `cd assets/dashboard && npm run dev`

If you need a development server with hot-reload during development:
```bash
cd assets/dashboard
npm run dev
```

But for production builds, always use `go run ./cmd/build-dashboard`.

## Development

- Source files: `src/`
- Public assets: `public/`
- Build output: `dist/` (consumed by Go binary)
