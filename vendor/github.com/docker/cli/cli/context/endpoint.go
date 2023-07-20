package context

// EndpointMetaBase contains fields we expect to be common for most context endpoints
type EndpointMetaBase struct {
	Host          string `json:",omitempty"`
	SkipTLSVerify bool
}
