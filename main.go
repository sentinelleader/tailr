package main

import (
	"flag"
	"log"
)

func main() {
	config := NewTailrConfig()
	config.watchDir = flag.String("logDir", "/tmp/logs", "path to log directory")
	config.logPattern = flag.String("logPattern", "*.log", "log pattern to look for")
	config.useOffset = flag.Bool("useOffset", true, "whether to use file offset method or not")
	config.offsetFilePath = flag.String("offsetFilePath", "/tmp/tailr-offset.json", "path to offset file store")
	config.noop = flag.Bool("noop", false, "prints the output to stdout")
	config.protocol = flag.String("protocol", "tcp", "tcp or udp")
	config.server = flag.String("server", "localhost", "server address")
	config.port = flag.String("port", "9000", "server port")
	flag.Parse()

	// If file offset is enabled, check for any existing offset file and if exists,
	// use the offset file to read the previous offsets
	if *config.useOffset {
		err := config.checkOffsetFile()
		if err != nil {
			log.Printf("Unable to read previous offset file: %v", err)
		}
	}

	config.watcherWaitgroup.Add(1)
	// Start the recursive Directory watcher
	go config.recursiveDirWatcher()

	// Start the signal handler
	go config.shutdownHandler()

	// Wait for the goroutines to finish
	config.watcherWaitgroup.Wait()

	// Update our offset file before shutting down
	config.updateOffsetFile()
	log.Println("Finished forwarding logs. Exiting ...")
}
