package migrations

import (
	"embed"
	"io/fs"
)

//go:embed mysql/*.sql
var files embed.FS
var FS fs.FS

func init() {
	var err error
	FS, err = fs.Sub(files, "mysql")
	if err != nil {
		panic(err)
	}
}
