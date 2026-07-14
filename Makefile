.PHONY: build-ui build test test-server test-admin-ui run

# build-ui builds the Admin UI (apps/admin-ui) to static assets, landing
# them directly under server/internal/adminui/dist (Vite's outDir, see
# apps/admin-ui/vite.config.ts) — the //go:embed target FD2 defines. Run
# this before `go build`/`go run` whenever apps/admin-ui has changed; the
# committed placeholder dist/index.html + .gitkeep let a clean checkout's Go
# build succeed even before this has ever run.
build-ui:
	cd apps/admin-ui && npm install && npm run build

# build runs build-ui first so the embedded Admin UI assets are always
# current, then builds the beecon binary.
build: build-ui
	cd server && go build ./...

# run builds the Admin UI, then runs the server (BEECON_* env vars must
# already be set, e.g. via server/.env.local).
run: build-ui
	cd server && go run ./cmd/beecon serve

test-server:
	cd server && go test ./...

test-admin-ui:
	cd apps/admin-ui && npm install && npm run test

test: test-server test-admin-ui
