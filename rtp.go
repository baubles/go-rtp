package rtp

import (
	"log"
	"os"
)

var logger = log.New(os.Stdout, "[rtp] ", log.LstdFlags)
