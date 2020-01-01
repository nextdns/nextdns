package internal

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func ReadOsRelease() (map[string]string, error) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return nil, err
	}
	o := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		idx := strings.IndexByte(line, '=')
		if idx == -1 {
			continue
		}
		o[line[:idx]], err = strconv.Unquote(line[idx+1:])
		if err != nil {
			continue
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return o, nil
}
