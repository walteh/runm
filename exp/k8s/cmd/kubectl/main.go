package main

import (
	"os"

	"k8s.io/kubectl/pkg/cmd"
)

func main() {
	if err := cmd.NewDefaultKubectlCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
