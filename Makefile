SPRYNCD_BIN = pkg/embedded/spryncd

.PHONY: all clean test spryncd sprync

all: spryncd sprync

spryncd:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	  go build -trimpath -ldflags="-s -w" \
	  -o $(SPRYNCD_BIN) ./cmd/spryncd

sprync: spryncd
	go build -trimpath -ldflags="-s -w" \
	  -o sprync ./cmd/sprync

clean:
	rm -f sprync $(SPRYNCD_BIN)

test:
	go test ./pkg/... -count=1
