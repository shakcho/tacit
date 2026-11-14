package main

import (
	"flag"
	"fmt"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print build version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	fmt.Println("Tacit API workspace is ready; the control-plane server is implemented by TAC-004.")
}
