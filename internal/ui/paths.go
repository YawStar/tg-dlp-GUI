package ui

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

// openFolder shows a folder in the system file manager.
func openFolder(dir string) error {
	u, err := url.Parse(storage.NewFileURI(dir).String())
	if err != nil {
		return err
	}
	app := fyne.CurrentApp()
	if app == nil {
		return errors.New("the application is not running")
	}
	return app.OpenURL(u)
}

// openLocalPath opens a file or folder with the OS default handler.
func openLocalPath(a fyne.App, p string) error {
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(p)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path // Windows drive paths: C:/... -> /C:/...
	}
	return a.OpenURL(u)
}
