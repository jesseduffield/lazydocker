package define

// ContainerSize holds the size of the container's root filesystem and top
// read-write layer.
type ContainerSize struct {
	RootFsSize int64 `json:"rootFsSize"`
	RwSize     int64 `json:"rwSize"`
}
