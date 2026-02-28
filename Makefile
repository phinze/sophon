.PHONY: build test clean frontend dev-frontend

frontend:
	cd server/frontend && npm ci && npx esbuild src/app.ts src/style.css \
		--bundle --outdir=../static --minify --target=es2020

build: frontend
	go build -o sophon .

test:
	go test ./...

clean:
	rm -f sophon
	rm -f server/static/*.js server/static/*.css server/static/*.map

dev-frontend:
	cd server/frontend && npx esbuild src/app.ts src/style.css \
		--bundle --outdir=../static --target=es2020 --watch
