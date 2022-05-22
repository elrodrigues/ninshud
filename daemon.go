package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"time"

	pb "github.com/elrodrigues/ninshud/jutsu"
	"github.com/hashicorp/memberlist"
	"github.com/sevlyar/go-daemon"
	"google.golang.org/grpc"
	// "github.com/golang/protobuf/proto"
)

const (
	pidFileName = "ninshud.pid"
	logFileName = "ninshud.log"
	signal_msg  = `Sends signal to Ninshu daemon.
Signals:
	stop  - gracefully stops the Ninshu daemon
	force - forces Ninshu daemon to stop
`
)

var (
	done                                 = make(chan struct{})
	port                                 = flag.Int("p", 47001, `Daemon's port number.`)
	s             *grpc.Server           = nil
	list          *memberlist.Memberlist = nil
	listAvailable                        = false
)

type clusterServices struct {
	pb.UnimplementedClusterServer
}

func (s *clusterServices) PingNode(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received ping: %v", in.GetPing())
	return &pb.HelloReply{Pong: "Hello " + in.GetPing()}, nil
}

func (s *clusterServices) DropAnchor(ctx context.Context, in *pb.ConnectRequest) (*pb.NinshuReply, error) {
	if list == nil {
		log.Println("dropping anchor...")
		// default port 7946, bound to all interfaces
		config := memberlist.DefaultLANConfig()
		config.AdvertiseAddr = in.GetHostIP()
		log.Printf("using %s\n", config.AdvertiseAddr)
		mlist, err := memberlist.Create(config)
		if err != nil {
			log.Printf("failed to create memberlist: %v", err)
			return &pb.NinshuReply{Success: false}, err
		}
		list = mlist
		listAvailable = true
		return &pb.NinshuReply{Success: true}, nil
	} else {
		return &pb.NinshuReply{Success: false}, nil
	}
}

func (s *clusterServices) RaiseAnchor(ctx context.Context, in *pb.EmptyRequest) (*pb.NinshuReply, error) {
	if list != nil {
		listAvailable = false
		log.Println("lifting anchor...")
		list.Leave(time.Second)
		if err := list.Shutdown(); err != nil {
			log.Println("!!! failed to leave Ninshu network !!!")
			return &pb.NinshuReply{Success: false}, err
		}
		list = nil
		return &pb.NinshuReply{Success: true}, nil
	} else {
		return &pb.NinshuReply{Success: false}, nil
	}
}

func (s *clusterServices) ConnectTo(ctx context.Context, in *pb.ConnectRequest) (*pb.NinshuReply, error) {
	if list == nil {
		log.Println("connecting to anchor...")
		// default port 7946, bound to all interfaces
		config := memberlist.DefaultLANConfig()
		config.AdvertiseAddr = in.GetHostIP()
		log.Printf("using %s\n", config.AdvertiseAddr)
		mlist, err := memberlist.Create(config)
		if err != nil {
			log.Printf("failed to create memberlist: %v", err)
			return &pb.NinshuReply{Success: false}, err
		}
		list = mlist
		n, err := list.Join([]string{in.GetIp()})
		if err != nil {
			log.Printf("failed to connect to anchor: %v", err)
			return &pb.NinshuReply{Success: false}, err
		}
		listAvailable = true
		reply := fmt.Sprintf("%d nodes were contacted", n)
		return &pb.NinshuReply{Success: true, Reply: &reply}, nil
	} else {
		return &pb.NinshuReply{Success: false}, nil
	}
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
	if list != nil {
		list.Leave(5 * time.Second)
		if err := list.Shutdown(); err != nil {
			log.Println("!!! failed to leave Ninshu network !!!")
		}
	}
	return daemon.ErrStop
}

func main() {
	// Register Flags
	signal := flag.String("s", "", signal_msg)
	flag.Parse()
	daemon.AddCommand(daemon.StringFlag(signal, "stop"), syscall.SIGQUIT, sigTermHandler)
	daemon.AddCommand(daemon.StringFlag(signal, "force"), syscall.SIGTERM, sigTermHandler)
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
