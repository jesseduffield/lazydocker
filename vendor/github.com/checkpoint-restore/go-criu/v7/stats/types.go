package stats

const (
	StatsDump    = "stats-dump"
	StatsRestore = "stats-restore"

	ImgServiceMagic = 0x55105940 /* Zlatoust */
	StatsMagic      = 0x57093306 /* Ostashkov */

	PrimaryMagicOffset   = 0x0
	SecondaryMagicOffset = 0x4
	SizeOffset           = 0x8
	PayloadOffset        = 0xC
)
