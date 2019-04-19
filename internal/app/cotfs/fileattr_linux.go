package cotfs

import (
	"os"
	"syscall"
	"time"
)

func getCreateTime(stat os.FileInfo) time.Time {
	return stat.ModTime()
	sysStat := stat.Sys().(*syscall.Stat_t)
	return time.Unix(int64(sysStat.Ctim.Sec), int64(sysStat.Ctim.Nsec))
}
