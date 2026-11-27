package main

import (
	"errors"
	"net/url"

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
