package syncer

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/event"
	"github.com/prysmaticlabs/geth-sharding/sharding/database"
	"github.com/prysmaticlabs/geth-sharding/sharding/mainchain"
	"github.com/prysmaticlabs/geth-sharding/sharding/p2p"
	"github.com/prysmaticlabs/geth-sharding/sharding/p2p/messages"
	"github.com/prysmaticlabs/geth-sharding/sharding/params"
	"github.com/prysmaticlabs/geth-sharding/sharding/types"
	"github.com/prysmaticlabs/geth-sharding/sharding/utils"
	log "github.com/sirupsen/logrus"
)

// Syncer represents a service that provides handlers for shard chain
// data requests/responses between remote nodes and event loops for
// performing windback sync across nodes, handling reorgs, and synchronizing
// items such as transactions and in future sharding iterations: state.
type Syncer struct {
	config       *params.Config
	client       *mainchain.SMCClient
	shardID      int
	shardChainDB *database.ShardDB
	p2p          *p2p.Server
	ctx          context.Context
	cancel       context.CancelFunc
	msgChan      chan p2p.Message
	bodyRequests event.Subscription
	errChan      chan error // Useful channel for handling errors at the service layer.
}

// NewSyncer creates a struct instance of a syncer service.
// It will have access to config, a signer, a p2p server,
// a shardChainDB, and a shardID.
func NewSyncer(config *params.Config, client *mainchain.SMCClient, p2p *p2p.Server, shardChainDB *database.ShardDB, shardID int) (*Syncer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error)
	return &Syncer{config, client, shardID, shardChainDB, p2p, ctx, cancel, nil, nil, errChan}, nil
}

// Start the main loop for handling shard chain data requests.
func (s *Syncer) Start() {
	log.Info("Starting sync service")

	shard := types.NewShard(big.NewInt(int64(s.shardID)), s.shardChainDB.DB())

	s.msgChan = make(chan p2p.Message, 100)
	s.bodyRequests = s.p2p.Feed(messages.CollationBodyRequest{}).Subscribe(s.msgChan)
	go s.HandleCollationBodyRequests(shard, s.ctx.Done())
	go utils.HandleServiceErrors(s.ctx.Done(), s.errChan)
}

// Stop the main loop.
func (s *Syncer) Stop() error {
	// Triggers a cancel call in the service's context which shuts down every goroutine
	// in this service.
	defer s.cancel()
	defer close(s.errChan)
	defer close(s.msgChan)
	log.Info("Stopping sync service")
	s.bodyRequests.Unsubscribe()
	return nil
}

// HandleCollationBodyRequests subscribes to messages from the shardp2p
// network and responds to a specific peer that requested the body using
// the Send method exposed by the p2p server's API (implementing the p2p.Sender interface).
func (s *Syncer) HandleCollationBodyRequests(collationFetcher types.CollationFetcher, done <-chan struct{}) {
	for {
		select {
		// Makes sure to close this goroutine when the service stops.
		case <-done:
			return
		case req := <-s.msgChan:
			if req.Data != nil {
				log.Infof("Received p2p request of type: %T", req)
				res, err := RespondCollationBody(req, collationFetcher)
				if err != nil {
					s.errChan <- fmt.Errorf("could not construct response: %v", err)
					continue
				}

				// Reply to that specific peer only.
				s.p2p.Send(*res, req.Peer)
				log.Infof("Responding to p2p request with collation with headerHash: %v", res.HeaderHash.Hex())
			}
		case <-s.bodyRequests.Err():
			s.errChan <- errors.New("subscriber failed")
			return
		}
	}
}
