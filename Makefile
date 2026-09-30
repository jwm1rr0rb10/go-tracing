NAMES=go-tracing

.PHONY: tags test bench lint vuln

test:
	go test -race -count=1 ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

tags:
	@bash -c ' \
		version=$$(cat "$(CURDIR)/version" 2>/dev/null || echo "0.0.0") && \
		echo "→ work with current directory and $$version" && \
		tag=v$$version && \
		echo "→ tag: $$tag" && \
		if [[ ! $$(git tag -l "$$tag") ]]; then \
			git tag -a "$$tag" -m "Release $$version" && \
			git push origin "$$tag" -o ci.skip && \
			echo "✅ Tagged and pushed $$tag"; \
		else \
			echo "⚠️  Tag $$tag already exists"; \
		fi \
	'
