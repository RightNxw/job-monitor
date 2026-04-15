module github.com/RightNxw/job-monitor/monitor

go 1.25.7

require (
	github.com/iancoleman/orderedmap v0.3.0
	github.com/joho/godotenv v1.5.1
	github.com/lib/pq v1.12.3
	github.com/sardanioss/httpcloak v1.5.10
	github.com/t14raptor/go-fast v0.0.6
	golang.org/x/net v0.50.0
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/miekg/dns v1.1.69 // indirect
	github.com/nukilabs/ftoa v1.0.0 // indirect
	github.com/nukilabs/unicodeid v0.1.0 // indirect
	github.com/sardanioss/http v1.1.0 // indirect
	github.com/sardanioss/net v1.2.1 // indirect
	github.com/sardanioss/qpack v0.6.2 // indirect
	github.com/sardanioss/quic-go v1.2.17 // indirect
	github.com/sardanioss/utls v1.10.1 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
)

// The solver (`-tags solver`) is commented out in this build, so its
// github.com/tommie/v8go requirement is intentionally absent. Running it needs
// a patched v8go build. See internal/cloudflare/stub.go.
