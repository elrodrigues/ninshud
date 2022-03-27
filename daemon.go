package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	// "github.com/hashicorp/memberlist"
	"github.com/sevlyar/go-daemon"
)

const (
	pidFileName = "ninshud.pid"
	logFileName = "ninshud.log"
	signal_msg  = `Sends signal to Ninshu daemon.
Signals:
	stop - stops the Ninshu daemon

`
)

var (
	stop = make(chan struct{})
	done = make(chan struct{})
)

// This function handles SIGTERM and SIGQUIT signals to the
// daemon. os.Signal is an interface implemented in the OS'
// version of Go. This uses Unix's syscall implementation.
func sigTermHandler(signal os.Signal) error {
	log.Println("terminating ninshu daemon...")
	stop <- struct{}{}
	if signal == syscall.SIGQUIT {
		<-done
	}
	return daemon.ErrStop
}

func main() {
	// Register Flags
	signal := flag.String("s", "", signal_msg)
	flag.Parse()
	daemon.AddCommand(daemon.StringFlag(signal, "stop"), syscall.SIGTERM, sigTermHandler)
	// Subtract umask from defaults 666 for files, 777 for folders
	context := &daemon.Context{
		PidFileName: pidFileName,
		PidFilePerm: 0644,
		LogFileName: logFileName,
		LogFilePerm: 0640,
		WorkDir:     "./", // bizarrely needs to be ./
		Umask:       027,  // Mask UserGroupWorld
		Args:        nil,
		// Credential:  ,
		// Env: ,
	}
	// Main thread handles flag and returns
	if len(daemon.ActiveFlags()) > 0 {
		process, err := context.Search()
		if err != nil {
			log.Fatalf("Unable to send signal. Is the daemon running?\n%s\n", err.Error())
		}
		daemon.SendCommands(process)
		return
	}
	// No flag to handle, start daemon
	if *signal != "" {
		fmt.Fprint(os.Stderr, signal_msg)
		os.Exit(2)
	}
	d, err := context.Reborn() // a fork
	if err != nil {
		log.Fatalln(err)
	}
	if d != nil { // d has pid
		return // Main thread returns
	}
	// CHILD STARTS HERE
	defer context.Release()
	log.Println("- - - - - - - - - - -")
	log.Println("Ninshu Daemon Started")
	// Setup goes here
	// Spawn worker
	go worker()

	err = daemon.ServeSignals() // blocks?
	if err != nil {
		log.Printf("SeveSignals error: %s", err.Error())
	}
	log.Println("Ninshu Daemon Stopped")
}

// Demo Worker
func worker() {
LOOP:
	for { // loop until SIGTERM received
		log.Println("+", time.Now().Unix())
		time.Sleep(time.Second)
		select {
		case <-stop:
			break LOOP
		default:
		}
	}
	done <- struct{}{}
}
