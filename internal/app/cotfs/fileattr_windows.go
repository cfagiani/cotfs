package cotfs

import (
	"os"
	"time"
)

func getCreateTime(stat os.FileInfo) time.Time {
	return stat.ModTime()
}
