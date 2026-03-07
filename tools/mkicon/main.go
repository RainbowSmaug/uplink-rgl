// mkicon generates cmd/uplink-host/rsrc_windows_amd64.syso, which embeds the
// app icon into the Windows executable so it appears in Explorer and the taskbar.
//
// Run from the module root:
//
//	go run ./tools/mkicon
package main

import (
	"fmt"
	"os"

	"github.com/rainbowsmaug/uplink-rgl/internal/icon"
	"github.com/tc-hib/winres"
)

func main() {
	out := "cmd/uplink-host/rsrc_windows_amd64.syso"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	imgs := icon.Images()
	ico, err := winres.NewIconFromImages(imgs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "icon error:", err)
		os.Exit(1)
	}

	rs := &winres.ResourceSet{}
	rs.SetIcon(winres.ID(1), ico)

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		fmt.Fprintln(os.Stderr, "write syso:", err)
		os.Exit(1)
	}

	fmt.Println("Generated", out)
}
