.PHONY: platform serve writer analyst researcher demo tidy build clean

# Start the platform server (registry + routing + dashboard)
platform:
	go run ./cmd/platform

# Same as platform, but via the ax CLI
serve:
	go run ./cmd/ax serve

# Start Company B's writer agent
writer:
	AX_PLATFORM_URL=http://localhost:8080 \
	AX_API_KEY=ax_companyb_demo \
	AX_AGENT_URL=http://localhost:8082 \
	AX_AGENT_PORT=8082 \
	go run ./examples/company-b-writer

# Start Company C's analyst agent
analyst:
	AX_PLATFORM_URL=http://localhost:8080 \
	AX_API_KEY=ax_companyc_demo \
	AX_AGENT_URL=http://localhost:8083 \
	AX_AGENT_PORT=8083 \
	go run ./examples/company-c-analyst

# Run Company A's researcher (queries platform, routes calls, prints results)
researcher:
	AX_PLATFORM_URL=http://localhost:8080 \
	AX_API_KEY=ax_companya_demo \
	go run ./examples/company-a-researcher

build:
	go build -o bin/ax-platform ./cmd/platform
	go build -o bin/ax ./cmd/ax

tidy:
	go mod tidy

clean:
	rm -rf bin/
