package main

import (
	"bufio"
	//"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	//"strings"
	"sync"
	"time"

	//"github.com/dbudworth/greak"
	"github.com/dustin/go-humanize"
	//"github.com/pkg/profile"
)

var clientCount = 0
var allClients = make(map[net.Conn]int)
var connLock sync.RWMutex

func runtimeStats(portNum string) {
	var m runtime.MemStats
	time.Sleep(5 * time.Second)
	for {
		runtime.ReadMemStats(&m)
		fmt.Println()
		fmt.Printf("Goroutines:\t%7d\t\t Clients:\t%7d\n", runtime.NumGoroutine(), clientCount)
		fmt.Printf("Last GC:%7d\t Next GC:\t%7s\n", m.LastGC, humanize.Bytes(m.NextGC))
		fmt.Printf("Heap from OS:\t%7s\t\t Heap Alloc:\t%7s\n", humanize.Bytes(m.HeapSys), humanize.Bytes(m.HeapAlloc))
		fmt.Printf("Free:\t%7d\t\t\t Heap Idle:\t%7s\n", m.Frees, humanize.Bytes(m.HeapIdle))
		fmt.Printf("Mallocs:\t%7d\t\t Live (m-f):\t%d\n", m.Mallocs, m.Mallocs-m.Frees)
		fmt.Printf("Heap Released:\t%7s\t\t Heap InUse:\t%7s\n", humanize.Bytes(m.HeapReleased), humanize.Bytes(m.HeapInuse))
		fmt.Println()

		time.Sleep(30 * time.Second)
	}
}

func forceGC() {
	//force garbage collection every 60 second
	for {
		time.Sleep(60 * time.Second)
		runtime.GC()
	}
}

func sendDataToClient(client net.Conn, msg []byte, ctx context.Context) {

	err := client.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		//fmt.Printf("\n\nSetWriteDeadline failed: %v\n\n", err)
		//removeFromConnMap(client)
		//return
	}

	select {
	case <-time.After(5 * time.Second):
		//fmt.Println("EJECTED!: ", client.RemoteAddr().String())
		removeFromConnMap(client)
		return
	case <-ctx.Done():
		//fmt.Println("On time: ",  client.RemoteAddr().String())
	}

	n, err := client.Write(msg)
	if err != nil {
		//log.Printf("Write ERR: Client will be %s disconnected \n", client.RemoteAddr().String())
		removeFromConnMap(client)

	} else if n != len(msg) {
		//log.Printf("Client connection did not accept expected number of bytes, %d != %d", n, len(msg))
		removeFromConnMap(client)

	}
}

func sendDataToClients(msg []byte) {
	// VRS ADSBx specific since no newline is printed between data bursts
	// we use ] and must add } closure
	msg = append(msg, []byte("}\r\n")...)

	ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)

	connLock.RLock()
	for client := range allClients {
		go sendDataToClient(client, msg, ctx)
	}
	connLock.RUnlock()

}

func removeFromConnMap(client net.Conn) {
	connLock.Lock()
	//log.Println("removing client", clientCount)
	delete(allClients, client)
	clientCount = len(allClients)
	client.Close()
	connLock.Unlock()
}

func addToConnMap(client net.Conn) {
	connLock.Lock()
	//log.Println("adding client", clientCount+1)
	allClients[client] = 0
	clientCount = len(allClients)
	connLock.Unlock()
}

func handleTCPIncoming(hostName string, portNum string) {
	conn, err := net.Dial("tcp", hostName+":"+portNum)
	// exit on TCP connect failure
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// constantly read JSON from PUB-VRS and write to the buffer
	data := bufio.NewReaderSize(conn, 5*1024*1024) // 5MB inbound read buffer, reused
	//i := 0
	for {
		scan, err := data.ReadSlice(']') // scan is a slice using the underlying read buffer array, not reallocated
		if len(scan) == 0 || err != nil {
			break
		}

		//if i == 1 { scan = scan[1:len(scan)] }

		if scan[0] == byte('}') { // trim the remaining close bracket
			scan = scan[1:] // adjust the start of the slice one byte, still using the read buffer array
		}
		msg := make([]byte, len(scan), len(scan)+3) // create the message slice, allocating a new array of exactly the needed capacity
		copy(msg, scan)                             // copy the bytes from the read buffer to the new slice

		go sendDataToClients(msg)
		//i = 1
	}
}

func handleTCPOutgoing(outportNum string) {
	// print error on listener error
	server, err := net.Listen("tcp", ":"+outportNum)
	if err != nil {
		//log.Fatalf("Listener err: %s\n", err)
	}

	for {
		incoming, err := server.Accept()
		// print error and continue waiting
		if err != nil {
			log.Println(err)
		} else {
			/*_, err := incoming.Write([]byte("{\"init\":\"ADSBx\"}"))
			if err != nil {
				log.Printf("Initial accept disconn: %s \n", incoming.RemoteAddr().String())
				removeFromConnMap(incoming)

			} else {*/
			//log.Printf("Initial conn: %s \n", incoming.RemoteAddr().String())
			go addToConnMap(incoming)
			/*}*/
		}

	}
}

func main() {

	//defer profile.Start(profile.MemProfile).Stop()
	//defer profile.Start(profile.MemProfileRate(1024)).Stop()

	//base := greak.New()
	//go func(){
	//	for {
	//	time.Sleep(60*time.Second)
	//	after := base.Check()
	//	fmt.Println("Sleeping goroutine should show here\n", after)
	//	}
	//}()

	hostName := flag.String("hostname", "", "what host to connect to")
	portNum := flag.String("port", "", "which port to connect with")
	outportNum := flag.String("listenport", "", "which port to listen on")
	flag.Parse()

	if *hostName == "" || *portNum == "" || *outportNum == "" {
		fmt.Println("usage: dial-tcp -hostname <input> -port <port> -listenport <output>")
		os.Exit(1)
	}

	//go runtimeStats(*portNum)
	go handleTCPOutgoing(*outportNum)

	// if this function returns, the main thread will exit, which exits the entire program
	handleTCPIncoming(*hostName, *portNum)
}
