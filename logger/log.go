package logger

import (
	"os"
	"path/filepath"
)

type logger struct {
}

var fs *os.File

func init() {
	return
	p, e := os.Executable()
	if e != nil {
		p = "./"
	} else {
		p = filepath.Dir(p) + "/"
	}
	f := p + "log.txt"
	ff, err := os.OpenFile(f, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return
	}
	fs = ff
}
func Info(s string) {
	if fs != nil {
		fs.WriteString(s)
	}
}
