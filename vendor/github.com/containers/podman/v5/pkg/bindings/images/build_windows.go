package images

import (
	"os"
)

func checkHardLink(_ os.FileInfo) (devino, bool) {
	return devino{}, false
}
