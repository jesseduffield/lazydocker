package define

// extra type to use as enum
type DiffType uint8

const (
	// only diff containers
	DiffContainer DiffType = 1 << iota
	// only diff images
	DiffImage
	// diff both containers and images
	DiffAll DiffType = 0b11111111
)

func (d DiffType) String() string {
	switch d {
	case DiffAll:
		return "all"
	case DiffContainer:
		return "container"
	case DiffImage:
		return "image"
	default:
		return "unknown"
	}
}
