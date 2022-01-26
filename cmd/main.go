package main

import (
	"flag"
	"gpipe/internal"
)

var (
	sType string
	sHost string

	eType string
	eHost string
)

func init() {
	flag.StringVar(&sType, "st", "tcp", "start type")
	flag.StringVar(&sHost, "sh", "localhost:8081", "start host")

	flag.StringVar(&eType, "et", "tcp", "end type")
	flag.StringVar(&eHost, "eh", "localhost:8082", "end host")

	flag.Parse()
}

func main() {
	p := &internal.Pipe{
		Start: internal.ConnCfg{
			Type: sType,
			Host: sHost,
		},
		End: internal.ConnCfg{
			Type: eType,
			Host: eHost,
		},
	}

	p.Handler()
}
