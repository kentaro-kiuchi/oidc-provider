package migration

import (
	"embed"
	"io/fs"
)

// Files contains only versioned schema migrations. The API binary does not
// import this package, so migration assets are not part of its executable.
//
//go:embed migrations/*.sql
var embeddedFiles embed.FS

var Files = mustSubFS()

func mustSubFS() fs.FS {
	files, err := fs.Sub(embeddedFiles, "migrations")
	if err != nil {
		panic(err)
	}
	return files
}
