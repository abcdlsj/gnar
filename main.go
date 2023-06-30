package main

import "github.com/abcdlsj/gpipe/cmd"

var gitHash string
var buildStamp string

func main() {
	cmd.Execute(gitHash, buildStamp)
}
