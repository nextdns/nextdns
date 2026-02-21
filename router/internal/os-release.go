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
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		o[before], err = strconv.Unquote(after)
		if err != nil {
			continue
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return o, nil
}
