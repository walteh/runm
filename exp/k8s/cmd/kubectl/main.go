package main

import "k8s.io/kubectl/pkg/cmd"

func main() {
	cmd.NewDefaultKubectlCommand().Execute()
}
