package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"
)

const (
	testDir  = "/tmp"
	testMsg1 = "FooBar"
	testMsg2 = "FooBaz"
)

var (
	isNoop         = true
	testLogPattern = "tailr_test_*"
	testOutputChan = make(chan string, 1)
)

func fakeOutputHandler(noopOutputChan <-chan string) {
	msg := <-noopOutputChan
	testOutputChan <- msg
}

func genTempDir(srcDir string) (string, error) {
	tempDir, err := ioutil.TempDir(srcDir, "tailr_testdir_")
	if err != nil {
		return "", err
	}

	return tempDir, nil
}

func genTempFile(srcDir string) (string, error) {
	tempFile, err := ioutil.TempFile(srcDir, "tailr_test_")
	if err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

func TestUpdateOffsetFile(t *testing.T) {

	tailrConfig := NewTailrConfig()

	// create a temp file for offset
	tempOffsetFile, err := genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}

	defer os.Remove(tempOffsetFile)
	tailrConfig.offsetFilePath = &tempOffsetFile
	tailrConfig.fileOffsetMap[tempOffsetFile] = 100

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
	tempOffsetFile, err := genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}
	tailrConfig.offsetFilePath = &tempOffsetFile

	// for empty offset file, checkOffsetFile() should raise error
	err = tailrConfig.checkOffsetFile()
	if err == nil {
		t.Error("Expected checkOffsetFile() to throw error for empty file")
	}
	os.Remove(tempOffsetFile)

	// if the offset file is defined but doesnt exists, checkOffsetFile() should raise error
	err = tailrConfig.checkOffsetFile()
	if err == nil {
		t.Error("Expected checkOffsetFile() to throw error if no offset file exists")
	}

	// create a temp file for offset
	tempOffsetFile, err = genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary file %v", err)
	}
	defer os.Remove(tempOffsetFile)
	tailrConfig.offsetFilePath = &tempOffsetFile

	// update offset file
	tailrConfig.fileOffsetMap[tempOffsetFile] = 100
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
	tempOffsetFile, err := genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary offset file %v", err)
	}
	tailrConfig.offsetFilePath = &tempOffsetFile
	tailrConfig.noop = &isNoop

	// create a temp file for offset
	tempLogFile, err := genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary log file %v", err)
	}

	defer func() {
		os.Remove(tempLogFile)
		// wait for the go routine to stop
		tailrConfig.watcherWaitgroup.Wait()
		os.Remove(tempOffsetFile)
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

func TestDirWatcher(t *testing.T) {
	tailrConfig := NewTailrConfig()

	// create a temp file for offset
	tempOffsetFile, err := genTempFile(testDir)
	if err != nil {
		t.Errorf("Error creating temporary offset file %v", err)
	}
	tailrConfig.offsetFilePath = &tempOffsetFile
	tailrConfig.noop = &isNoop
	tailrConfig.logPattern = &testLogPattern

	// create a temp directory to watch
	tempLogDir, err := genTempDir(testDir)
	if err != nil {
		t.Errorf("Error creating temporary log directory %v", err)
	}

	defer func() {
		tailrConfig.Kill(nil)
		// wait for the go routine to stop
		tailrConfig.watcherWaitgroup.Wait()
		os.RemoveAll(tempLogDir)
		os.Remove(tempOffsetFile)
	}()

	// spin up the fake output handler
	go fakeOutputHandler(tailrConfig.noopOutputChan)

	// spin up the dirwatcher for the temp log directory
	tailrConfig.watcherWaitgroup.Add(1)
	go tailrConfig.dirWatcher(tempLogDir)

	// create a temp log file inside our temp log directory
	tempLogFile, err := genTempFile(tempLogDir)
	if err != nil {
		t.Errorf("Error creating temporary log file %v", err)
	}

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
