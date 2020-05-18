package discovery

import (
	"os"
	"time"
)

type fileInfo struct {
	path  string
	mtime time.Time
	size  int64
}

func getFileInfo(path string) (fi fileInfo, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return fi, err
	}
	fi.path = path
	fi.mtime = st.ModTime()
	fi.size = st.Size()
	return
}

func (fi fileInfo) Equal(path string) bool {
	if fi.path != path {
		return false
	}
	fi2, err := getFileInfo(path)
	return err == nil && fi.mtime.Equal(fi2.mtime) && fi.size == fi2.size
}
