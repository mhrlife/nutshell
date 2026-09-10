BINARY := nutshell
MAX_LINES := 500

.PHONY: build run test lint lint-fix fmt check-file-length icons ci clean

build:
	go build -o $(BINARY) ./cmd/nutshell

run: build
	./$(BINARY) --no-open

test:
	go test -race ./...

lint: check-file-length
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...

fmt:
	golangci-lint fmt ./...

# Go files are capped by revive's file-length-limit rule in .golangci.yml;
# this target applies the same cap to every hand-written source file.
check-file-length:
	@fail=0; \
	for f in $$(git ls-files '*.go' '*.js' '*.css' '*.html' 2>/dev/null || find . -name '*.go' -o -name '*.js' -o -name '*.css' -o -name '*.html'); do \
		case "$$f" in */vendor/*) continue;; esac; \
		n=$$(wc -l < "$$f"); \
		if [ "$$n" -gt $(MAX_LINES) ]; then echo "$$f: $$n lines (max $(MAX_LINES))"; fail=1; fi; \
	done; \
	exit $$fail

# assets/logo.svg is the master mark; the UI copy and the three raster icons
# are generated from it. Needs ImageMagick.
icons:
	cp assets/logo.svg internal/web/static/logo.svg
	@tmp=$$(mktemp -d); \
	convert -background none -density 1200 assets/logo.svg -trim +repage -resize 960x960 "$$tmp/mark.png"; \
	for spec in favicon.png:32 favicon-192.png:192 apple-touch-icon.png:180; do \
		out=$${spec%%:*}; size=$${spec##*:}; \
		convert "$$tmp/mark.png" -background none -gravity center -extent 1024x1024 \
			-resize $${size}x$${size} -depth 8 -strip "PNG32:internal/web/static/$$out"; \
	done; \
	rm -rf "$$tmp"

ci: lint test build

clean:
	rm -f $(BINARY)
