package cotfs

import (
	"os"
	"syscall"
	"time"
)

func getCreateTime(stat os.FileInfo) time.Time {
	sysStat := stat.Sys().(*syscall.Stat_t)
	return time.Unix(int64(sysStat.Ctimespec.Sec), int64(sysStat.Ctimespec.Nsec))
}
