package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"
)

const (
	testMsg1 = "FooBar"
	testMsg2 = "FooBaz"
)

var (
	IsNoop         = true
	testOutputChan = make(chan string, 1)
)

func fakeOutputHandler(noopOutputChan <-chan string) {
	msg := <-noopOutputChan
	testOutputChan <- msg
}

func genTempFile() (string, error) {
	tempFile, err := ioutil.TempFile("/tmp", "tailr_test_")
	if err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

func TestUpdateOffsetFile(t *testing.T) {

	tailrConfig := NewTailrConfig()

	// create a temp file for offset
	tempFile, err := genTempFile()
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}

	defer os.Remove(tempFile)
	tailrConfig.offsetFilePath = &tempFile
	tailrConfig.fileOffsetMap[tempFile] = 100

	tailrConfig.updateOffsetFile()

	newOffsetMap, err := ioutil.ReadFile(*tailrConfig.offsetFilePath)
	if err != nil {
		t.Errorf("Error reading from the temporary offset file %v", err)
	}

	json, err := json.Marshal(tailrConfig.fileOffsetMap)
	if err != nil {
		t.Errorf("Error marshalling offsetMap %v", err)
	}

	// check if the offsets in memory and file are same
	if !bytes.Equal(newOffsetMap, json) {
		t.Errorf("Error updating offset file. Expected %v, got %v", string(newOffsetMap), string(json))
	}
}

func TestCheckOffsetFile(t *testing.T) {
	tailrConfig := NewTailrConfig()

	// create a temp file for offset
	tempFile, err := genTempFile()
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}
	tailrConfig.offsetFilePath = &tempFile

	// for empty offset file, checkOffsetFile() should raise error
	err = tailrConfig.checkOffsetFile()
	if err == nil {
		t.Error("Expected checkOffsetFile() to throw error for empty file")
	}
	os.Remove(tempFile)

	// if the offset file is defined but doesnt exists, checkOffsetFile() should raise error
	err = tailrConfig.checkOffsetFile()
	if err == nil {
		t.Error("Expected checkOffsetFile() to throw error if no offset file exists")
	}

	// create a temp file for offset
	tempFile, err = genTempFile()
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}
	defer os.Remove(tempFile)
	tailrConfig.offsetFilePath = &tempFile

	// update offset file
	tailrConfig.fileOffsetMap[tempFile] = 100
	tailrConfig.updateOffsetFile()

	// For a valid offset file, checkOffsetFile() shouldn't raise any error
	err = tailrConfig.checkOffsetFile()
	if err != nil {
		t.Errorf("Error checking offset file %v", err)
	}
}

func TestStartFileForwarder(t *testing.T) {
	tailrConfig := NewTailrConfig()

	// create a temp file for offset
	tempFile, err := genTempFile()
	if err != nil {
		t.Errorf("Error creating temporary offset file %v", err)
	}
	tailrConfig.offsetFilePath = &tempFile
	tailrConfig.noop = &IsNoop

	// create a temp file for offset
	tempLogFile, err := genTempFile()
	if err != nil {
		t.Errorf("Error creating temporary log file %v", err)
	}

	defer func() {
		os.Remove(tempLogFile)
		os.Remove(tempFile)
		// wait for the go routine to stop
		tailrConfig.watcherWaitgroup.Wait()
	}()

	// spin up the fake output handler
	go fakeOutputHandler(tailrConfig.noopOutputChan)

	// spin up the file forwarder
	tailrConfig.watcherWaitgroup.Add(1)
	go tailrConfig.startFileForwarder(tempLogFile)

	// lets write something to the temporary log file
	f, err := os.OpenFile(tempLogFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Errorf("Error opening temp log file %v", err)
	}

	_, err = f.Write([]byte(testMsg1 + "\n"))
	if err != nil {
		t.Errorf("Error writing to temp log file %v", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Error closing temp log file %v", err)
	}

	// get the message from the fake output handler and compare
	output := <-testOutputChan
	if output != testMsg1 {
		t.Errorf("Message mismatch. Expected %v and got %v", testMsg1, output)
	}
}
