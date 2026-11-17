package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "Axon-x",
		Width:     1280,
		Height:    860,
		MinWidth:  960,
		MinHeight: 640,
		// macOS restores the last window frame, and a frame that ended up
		// off-screen (dragged above the menu bar, or onto a display that is no
		// longer attached) makes the app look like it never started. domReady
		// re-centres it on every launch.
		WindowStartState: options.Normal,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
