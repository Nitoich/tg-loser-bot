# /bin/bash

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

export CGO_CFLAGS="-I$SCRIPT_DIR/td/tdlib/include"
export CGO_LDFLAGS="-L$SCRIPT_DIR/td/tdlib/lib -ltdjson"
export LD_LIBRARY_PATH="$SCRIPT_DIR/td/tdlib/lib:$LD_LIBRARY_PATH"

go run main.go