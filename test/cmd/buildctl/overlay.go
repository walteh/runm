//go:build overlaygen

package main

import (
	// when using overlaygen, this creates an overlay.json file that points
	// to a modified version of the runc package so we can import it as a package

	//go:overlay
	_ "github.com/moby/buildkit/cmd/buildctl"
)
