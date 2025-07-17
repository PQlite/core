package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// P2PMessage defines the structure of data sent over the network.
// It includes the number and a timestamp for latency calculation.
type P2PMessage struct {
	Number    int
	Timestamp int64
}

// SharedList is a thread-safe list of integers.
type SharedList struct {
	mu      sync.Mutex
	Numbers []int
}

// Add appends a number to the list in a thread-safe way.
func (sl *SharedList) Add(number int) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.Numbers = append(sl.Numbers, number)
}

// GetNumbers returns a copy of the current list.
func (sl *SharedList) GetNumbers() []int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	// Return a copy to avoid race conditions outside the lock
	numbersCopy := make([]int, len(sl.Numbers))
	copy(numbersCopy, sl.Numbers)
	return numbersCopy
}

// Node sets up and runs the P2P node.
func Node() {
	ctx := context.Background()

	// Create a new libp2p host
	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.EnableRelayService(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		log.Fatalf("Failed to create host: %v", err)
	}
	defer host.Close()

	log.Printf("[INFO] Your Peer ID is: %s", host.ID())
	log.Println("[INFO] Listening addresses:")
	for _, addr := range host.Addrs() {
		log.Printf("  %s/p2p/%s", addr, host.ID())
	}

	// Setup peer discovery
	if err := setupDiscovery(ctx, host); err != nil {
		log.Fatalf("Failed to setup discovery: %v", err)
	}

	// Setup Pub/Sub
	ps, err := pubsub.NewGossipSub(ctx, host)
	if err != nil {
		log.Fatalf("Failed to create PubSub: %v", err)
	}

	// Join the topic for our shared list
	topic, err := ps.Join("pqlite-shared-list")
	if err != nil {
		log.Fatalf("Failed to join topic: %v", err)
	}

	// Create the shared list and subscribe to the topic
	sharedList := &SharedList{}
	sub, err := topic.Subscribe()
	if err != nil {
		log.Fatalf("Failed to subscribe to topic: %v", err)
	}

	// Start a goroutine to handle incoming messages
	go handleMessages(ctx, sub, host.ID(), sharedList)

	// Start a goroutine to read user input from the console
	go readConsoleInput(ctx, topic, sharedList)

	log.Println("[INFO] Node is running. Type a number and press Enter to broadcast.")
	// Block forever
	select {}
}

// handleMessages processes incoming messages from the Pub/Sub topic.
func handleMessages(ctx context.Context, sub *pubsub.Subscription, selfID peer.ID, list *SharedList) {
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			log.Printf("Error reading from subscription: %v", err)
			continue
		}

		// Ignore messages sent by ourselves
		if msg.ReceivedFrom == selfID {
			continue
		}

		var p2pMsg P2PMessage
		if err := json.Unmarshal(msg.Data, &p2pMsg); err != nil {
			log.Printf("Error unmarshalling message: %v", err)
			continue
		}

		// Calculate latency
		latency := time.Now().UnixNano() - p2pMsg.Timestamp
		latencyMs := float64(latency) / float64(time.Millisecond)

		// Add the number to the shared list
		list.Add(p2pMsg.Number)

		// Log received message info. The extra newline at the start and the `> ` at the end
		// help keep the console output clean.
		log.Printf("\n[<- RECV] Number: %d from %s", p2pMsg.Number, msg.ReceivedFrom.ShortString())
		log.Printf("         Network Latency: %.2f ms", latencyMs)
		log.Printf("         Updated List: %v\n> ", list.GetNumbers())
	}
}

// readConsoleInput reads numbers from the console and broadcasts them.
func readConsoleInput(ctx context.Context, topic *pubsub.Topic, list *SharedList) {
	reader := bufio.NewReader(os.Stdin)
	for {
		log.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		number, err := strconv.Atoi(input)
		if err != nil {
			log.Printf("Invalid input. Please enter an integer.")
			continue
		}

		// Add to our own list first
		list.Add(number)
		log.Printf("   Updated List: %v", list.GetNumbers())

		// Create and publish the message
		msg := P2PMessage{
			Number:    number,
			Timestamp: time.Now().UnixNano(),
		}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Error marshalling message: %v", err)
			continue
		}

		if err := topic.Publish(ctx, msgBytes); err != nil {
			log.Printf("Error publishing message: %v", err)
		}
	}
}

// setupDiscovery configures the Kademlia DHT for peer discovery.
func setupDiscovery(ctx context.Context, h host.Host) error {
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		return err
	}
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return err
	}

	// Connect to default bootstrap peers
	for _, peerAddr := range dht.DefaultBootstrapPeers {
		addr, _ := peer.AddrInfoFromString(peerAddr.String())
		if err := h.Connect(ctx, *addr); err != nil {
			// This can be noisy, so we can comment it out.
			// log.Printf("Failed to connect to bootstrap peer: %v", err)
		} else {
			log.Printf("[+] Connected to bootstrap peer: %s", addr.ID.ShortString())
		}
	}

	// Announce our presence and search for others
	routingDiscovery := routing.NewRoutingDiscovery(kademliaDHT)
	util.Advertise(ctx, routingDiscovery, "pqlite-network")

	go func() {
		for {
			log.Println("[-] Searching for other peers...")
			peerChan, err := routingDiscovery.FindPeers(ctx, "pqlite-network")
			if err != nil {
				log.Printf("Failed to find peers: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}
			for p := range peerChan {
				if p.ID == h.ID() || len(p.Addrs) == 0 {
					continue
				}
				if err := h.Connect(ctx, p); err != nil {
					// This can be noisy, so we can comment it out.
					// log.Printf("Failed to connect to peer %s: %v", p.ID.ShortString(), err)
				} else {
					log.Printf("[++] Connected to peer: %s", p.ID.ShortString())
				}
			}
			time.Sleep(30 * time.Second) // Search less frequently once running
		}
	}()

	return nil
}

