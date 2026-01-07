package mkcw

import _ "embed"

//go:embed "embed/entrypoint_amd64.gz"
var entrypointCompressedBytes []byte
