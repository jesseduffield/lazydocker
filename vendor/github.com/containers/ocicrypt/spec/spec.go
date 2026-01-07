package spec

const (
	// MediaTypeLayerEnc is MIME type used for encrypted layers.
	MediaTypeLayerEnc = "application/vnd.oci.image.layer.v1.tar+encrypted"
	// MediaTypeLayerGzipEnc is MIME type used for encrypted gzip-compressed layers.
	MediaTypeLayerGzipEnc = "application/vnd.oci.image.layer.v1.tar+gzip+encrypted"
	// MediaTypeLayerZstdEnc is MIME type used for encrypted zstd-compressed layers.
	MediaTypeLayerZstdEnc = "application/vnd.oci.image.layer.v1.tar+zstd+encrypted"
	// MediaTypeLayerNonDistributableEnc is MIME type used for non distributable encrypted layers.
	MediaTypeLayerNonDistributableEnc = "application/vnd.oci.image.layer.nondistributable.v1.tar+encrypted"
	// MediaTypeLayerNonDistributableGzipEnc is MIME type used for non distributable encrypted gzip-compressed layers.
	MediaTypeLayerNonDistributableGzipEnc = "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip+encrypted"
	// MediaTypeLayerNonDistributableZstdEnc is MIME type used for non distributable encrypted zstd-compressed layers.
	MediaTypeLayerNonDistributableZstdEnc = "application/vnd.oci.image.layer.nondistributable.v1.tar+zstd+encrypted"
	// MediaTypeLayerNonDistributableZsdtEnc is MIME type used for non distributable encrypted zstd-compressed layers.
	//
	// Deprecated: Use [MediaTypeLayerNonDistributableZstdEnc].
	MediaTypeLayerNonDistributableZsdtEnc = MediaTypeLayerNonDistributableZstdEnc
)
