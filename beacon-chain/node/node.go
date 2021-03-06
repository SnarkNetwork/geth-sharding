package node

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/prysmaticlabs/geth-sharding/shared"
	"github.com/prysmaticlabs/geth-sharding/shared/debug"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// BeaconNode defines a struct that handles the services running a random beacon chain
// full PoS node. It handles the lifecycle of the entire system and registers
// services to a service registry.
type BeaconNode struct {
	services *shared.ServiceRegistry
	lock     sync.RWMutex
	stop     chan struct{} // Channel to wait for termination notifications.
}

// New creates a new node instance, sets up configuration options, and registers
// every required service to the node.
func New(ctx *cli.Context) (*BeaconNode, error) {
	registry := shared.NewServiceRegistry()
	beacon := &BeaconNode{
		services: registry,
		stop:     make(chan struct{}),
	}

	return beacon, nil
}

// Start the BeaconNode and kicks off every registered service.
func (b *BeaconNode) Start() {
	b.lock.Lock()

	log.Info("Starting beacon node")

	b.services.StartAll()

	stop := b.stop
	b.lock.Unlock()

	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigc)
		<-sigc
		log.Info("Got interrupt, shutting down...")
		go b.Close()
		for i := 10; i > 0; i-- {
			<-sigc
			if i > 1 {
				log.Info("Already shutting down, interrupt more to panic.", "times", i-1)
			}
		}
		debug.Exit() // Ensure trace and CPU profile data are flushed.
		panic("Panic closing the beacon node")
	}()

	// Wait for stop channel to be closed.
	<-stop
}

// Close handles graceful shutdown of the system.
func (b *BeaconNode) Close() {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.services.StopAll()
	log.Info("Stopping beacon node")
	close(b.stop)
}
