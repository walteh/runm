package vmmtest

import (
	"flag"
)

var (
	testName string
)

func init() {
	flag.StringVar(&testName, "test-name", "", "The name of the test to run")
	flag.Parse()
}

func main() {
	// ctx := context.Background()

}
