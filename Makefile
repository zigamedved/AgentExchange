.PHONY: platform writer analyst researcher demo tidy build clean

# Start the platform server (registry + routing + dashboard)
platform:
	go run ./cmd/platform

# Start Company B's writer agent
writer:
	FAXP_PLATFORM_URL=http://localhost:8080 \
	FAXP_API_KEY=faxp_companyb_demo \
	FAXP_AGENT_URL=http://localhost:8082 \
	FAXP_AGENT_PORT=8082 \
	go run ./examples/company-b-writer

# Start Company C's analyst agent
analyst:
	FAXP_PLATFORM_URL=http://localhost:8080 \
	FAXP_API_KEY=faxp_companyc_demo \
	FAXP_AGENT_URL=http://localhost:8083 \
	FAXP_AGENT_PORT=8083 \
	go run ./examples/company-c-analyst

# Run Company A's researcher (queries platform, routes calls, prints results)
researcher:
	FAXP_PLATFORM_URL=http://localhost:8080 \
	FAXP_API_KEY=faxp_companya_demo \
	go run ./examples/company-a-researcher

build:
	go build -o bin/faxp-platform ./cmd/platform
	go build -o bin/fixctl ./cmd/fixctl

tidy:
	go mod tidy

clean:
	rm -rf bin/
