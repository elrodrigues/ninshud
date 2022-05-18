package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"

	// "time"

	// "github.com/hashicorp/memberlist"
	pb "github.com/elrodrigues/ninshud/jutsu"
	"github.com/sevlyar/go-daemon"
	"google.golang.org/grpc"
	// "github.com/golang/protobuf/proto"
	// "github.com/elrodrigues/ninshud/clientRPC"
)

const (
	pidFileName = "ninshud.pid"
	logFileName = "ninshud.log"
	signal_msg  = `Sends signal to Ninshu daemon.
Signals:
	stop - stops the Ninshu daemon
	ping - test and stop Ninshu daemon
`
)

var (
	done              = make(chan struct{})
	port              = flag.Int("p", 47001, `Daemon's port number. Default is 47001`)
	s    *grpc.Server = nil
)

type clusterServices struct {
	pb.UnimplementedClusterServer
}

func (s *clusterServices) PingNode(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received ping: %v", in.GetPing())
	return &pb.HelloReply{Pong: "Hello " + in.GetPing()}, nil
}

// This function handles SIGTERM and SIGQUIT signals to the
// daemon. os.Signal is an interface implemented in the OS'
// version of Go. This uses Unix's syscall implementation.
func sigTermHandler(signal os.Signal) error {
	log.Println("terminating ninshu daemon...")
	// stop <- struct{}{}
	if s == nil {
		log.Println("child process has not started server!")
		return nil
	}
	s.GracefulStop()
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
	// Spawn workers
	go worker()

	err = daemon.ServeSignals() // blocks?
	if err != nil {
		log.Printf("ServeSignals error: %s", err.Error())
	}
	log.Println("Ninshu Daemon Stopped")
}

// gRPC Worker
func worker() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen at %d: %v", *port, err)
	}
	s = grpc.NewServer()
	pb.RegisterClusterServer(s, &clusterServices{})
	log.Printf("server listening at %v\n", lis.Addr())
	if err = s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	done <- struct{}{}
}
