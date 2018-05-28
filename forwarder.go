package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hpcloud/tail"
	"github.com/hpcloud/tail/watch"
	"gopkg.in/tomb.v1"
)

type tailrConfig struct {
	tomb.Tomb              // provides: Done, Kill, Dying
	watcherWaitgroup       sync.WaitGroup
	watcherMutex           sync.Mutex
	watchDir, logPattern   *string
	offsetFilePath         *string
	useOffset              *bool
	fileOffsetMap          map[string]int64
	noop                   *bool
	outputChan             chan string
	noopOutputChan         chan string
	protocol, server, port *string
	conn                   net.Conn
}

func NewTailrConfig() *tailrConfig {
	return &tailrConfig{
		fileOffsetMap:  make(map[string]int64),
		outputChan:     make(chan string, 10),
		noopOutputChan: make(chan string, 10),
	}
}

func (c *tailrConfig) outputHandler() {
OutputHandlerLoop:
	for {
		select {
		case n := <-c.noopOutputChan:
			fmt.Println(n)
		case o := <-c.outputChan:
			fmt.Fprintf(c.conn, o+"\n")
		case <-c.Dying():
			log.Printf(
				"Stopping noop output handler as the process received a stop signal",
			)
			break OutputHandlerLoop
		}
	}
	defer func() {
		if c.conn != nil {
			c.conn.Close()
		}
		close(c.outputChan)
		close(c.noopOutputChan)
		c.watcherWaitgroup.Done()
	}()
}

func (c *tailrConfig) shutdownHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)
	<-sigChan
	log.Println("Received Shutdown signal, triggering exit ...")
	// Notify the Tail that we are going to stop
	c.Kill(nil)
	defer func() {
		close(sigChan)
		c.watcherWaitgroup.Done()
	}()
}

func (c *tailrConfig) updateOffsetFile() {
	c.watcherMutex.Lock()
	defer c.watcherMutex.Unlock()
	offsetJson, err := json.Marshal(c.fileOffsetMap)
	if err != nil {
		log.Printf("Error Marshaling File Offset Metadata")
		return
	}
	err = ioutil.WriteFile(*c.offsetFilePath, []byte(offsetJson), 0644)
	if err != nil {
		log.Println("Error updating Offset metadata File")
		return
	}
}

func (c *tailrConfig) checkOffsetFile() error {
	fi, err := os.Stat(*c.offsetFilePath)
	if err != nil {
		return fmt.Errorf("Failed to collect file stat for file %s: %v", *c.offsetFilePath, err)
	}
	if fi.IsDir() {
		return errors.New("Error Offset File path belongs to directory not file")
	}
	offsetMap, err := ioutil.ReadFile(*c.offsetFilePath)
	if err != nil {
		return fmt.Errorf("Error reading offSet File %s: %v", *c.offsetFilePath, err)
	}
	err = json.Unmarshal(offsetMap, &c.fileOffsetMap)
	if err != nil {
		return errors.New("Error Unmarshalling json file offset data")
	}

	return nil
}

func (c *tailrConfig) startFileForwarder(f string) {
	defer c.watcherWaitgroup.Done()

	tailConfig := tail.Config{Follow: true, Poll: true, MustExist: true}
	c.watcherMutex.Lock()
	offset, ok := c.fileOffsetMap[f]
	c.watcherMutex.Unlock()
	if ok {
		tailConfig.Location = &tail.SeekInfo{Offset: offset, Whence: 1}
	}
	t, err := tail.TailFile(f, tailConfig)
	if err != nil {
		log.Printf("Failed to start tailing %s: %v", f, err)
		return
	}
	// Start the background stopper and offset writer
	fileWatcher := watch.NewPollingFileWatcher(f)
	fi, err := os.Stat(f)
	if err != nil {
		log.Println("Failed to collect file stat for file %s", f)
		return
	}
	size := fi.Size()
	fileChange, err := fileWatcher.ChangeEvents(&t.Tomb, size)
	if err != nil {
		log.Println(err)
	}
	timer := time.NewTicker(time.Second * 20)
ForwarderLoop:
	for {
		select {
		case <-timer.C:
			if t == nil {
				// oops looks like the file was removed
				continue
			}
			offset, err := t.Tell()
			if err != nil {
				log.Printf(
					"Error collecting file offset for %s",
					f,
				)
				continue
			}
			c.watcherMutex.Lock()
			c.fileOffsetMap[f] = offset
			c.watcherMutex.Unlock()
			log.Printf("offet updated for file %v", f)
		case <-fileChange.Deleted:
			log.Printf(
				"Stopping File Forwarder for %v as the file was removed",
				f,
			)
			// Remove the key from the file Offset Map
			c.watcherMutex.Lock()
			delete(c.fileOffsetMap, f)
			c.watcherMutex.Unlock()
			t.Stop()
			break ForwarderLoop
		case <-c.Dying():
			log.Printf(
				"Stopping File Forwarder for %v as the process received a stop signal",
				f,
			)
			t.Stop()
			break ForwarderLoop
		case lines := <-t.Lines:
			// Hmm, sometimes we get empty Line struct :/ mostly when the file gets deleted
			if lines == nil {
				continue
			} else if *c.noop {
				c.noopOutputChan <- lines.Text
				continue
			}
			c.outputChan <- lines.Text
		}
	}
}

func (c *tailrConfig) dirWatcher(dir string) {
	defer c.watcherWaitgroup.Done()
	// Start log forwarder for existing log files
	match, err := filepath.Glob(path.Join(dir, *c.logPattern))
	if err != nil {
		log.Printf("Error finding files inside %s: %v", dir, err)
		return
	}
	for _, f := range match {
		log.Printf("Starting File Forwarder for %s", f)
		c.watcherWaitgroup.Add(1)
		go c.startFileForwarder(f)
	}

	// start the watcher for new files and dirs taht gets created
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating watcher for %s: %v", dir, err)
		return
	}
	err = watcher.Add(dir)
	if err != nil {
		log.Printf("Error starting watcher for %s: %v", dir, err)
		watcher.Close()
		return
	}

	// Now listen for the events
DirWatchLoop:
	for {
		select {
		case event := <-watcher.Events:
			if event.Op.String() == "CREATE" {
				log.Println("Watcher Event:", event.Name, event.Op)
				fi, err := os.Stat(event.Name)
				if err != nil {
					log.Printf("Failed to collect file stat for file %s", event.Name)
					continue
				}
				if fi.IsDir() {
					// A new directory, lets start the directory watcher
					c.watcherWaitgroup.Add(1)
					go c.dirWatcher(event.Name)
					continue
				}
				// A new file, check if it matches the regex
				// Replace * with some valid characters to work with regexp
				filePattern := strings.Replace(*c.logPattern, "*", "[0-9a-zA-Z@_-]+", -1) + "$"
				matched, err := regexp.MatchString(filePattern, event.Name)
				if err != nil {
					log.Printf("Error matching regex with pattern %s for file name %s", filePattern, event.Name)
					continue
				}
				if !matched {
					log.Printf("File %s did not match the pattern %s", event.Name, filePattern)
					continue
				}
				// File name matched the pattern, lets start forwarding
				log.Printf("Starting File Forwarder for %s", event.Name)
				c.watcherWaitgroup.Add(1)
				go c.startFileForwarder(event.Name)
			}
			if event.Op.String() == "REMOVE" {
				// Ouch the directory got removed, lets stop the corresponding
				// directory watcher
				if event.Name == dir {
					log.Println("Watcher Event:", event.Name, event.Op)
					log.Printf(
						"Stopping Directory Watcher for %s as the directory was removed",
						dir,
					)
					watcher.Close()
					break DirWatchLoop
				}
			}
		case err := <-watcher.Errors:
			log.Println("Watcher Error:", err)
		case <-c.Dying():
			log.Printf(
				"Stopping Directory watcher for %s as the process received a stop signal",
				dir,
			)
			watcher.Close()
			break DirWatchLoop
		}
	}
}

func (c *tailrConfig) recursiveDirWatcher() {
	defer c.watcherWaitgroup.Done()
	if !*c.noop {
		// start the output server connection
		var err error
		c.conn, err = net.Dial(
			*c.protocol,
			fmt.Sprintf("%s:%s", *c.server, *c.port),
		)
		if err != nil {
			panic(errors.New(fmt.Sprintf("Unable to connect to Server %s on Port %s", *c.server, *c.port)))
		}
	}
	dirs, err := ioutil.ReadDir(*c.watchDir)
	if err != nil {
		log.Printf("Unable to read the watch directory %s", *c.watchDir)
		return
	}

	c.watcherWaitgroup.Add(3)

	// Start the signal handler
	go c.shutdownHandler()

	// start the output handler
	go c.outputHandler()

	// start the directory watchers
	go c.dirWatcher(*c.watchDir)

	for _, dir := range dirs {
		if dir.IsDir() {
			log.Printf("Starting Directory Watcher for %s", dir.Name())
			c.watcherWaitgroup.Add(1)
			// Watch each of the sub folders
			go c.dirWatcher(path.Join(*c.watchDir, dir.Name()))
		}
	}
}
