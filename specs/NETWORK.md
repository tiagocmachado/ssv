# SSV Specification - Network

**WIP**

This document contains the networking specification for SSV.
    
## Fundamentals

### Stack

SSV is a decentralized P2P network, built with [Libp2p](https://libp2p.io/), a modular framework for P2P networking. \
`discv5` is a complementary module used for decentralized discovery in the network.

### Transport

All nodes in the network should support both `TCP` and `UDP`. \
`TCP` is used by libp2p for setting up communication channels between peers. \
`UDP` is used by `discv5` for discovery purposes. \
Both ports should be specified in the node's ENR. 

[go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise) is used to secure transport channels ([noise protocol](https://noiseprotocol.org/noise.html)).

Multiplexing of protocols over channels is achieved using [yamux](https://github.com/libp2p/go-libp2p-yamux) protocol.

### Identity

There are two keys for each peer in the network:

##### Network Key
`Network Key` is used to create peer ID. \
All messages from a peer are signed using this key and verified by other peers with the corresponding public key. \
Unless provided in configuration (`NetworkPrivateKey` / `NETWORK_PRIVATE_KEY`), the key will be generated and saved locally for future use. 

##### Operator Key
`Operator Key` is used for decryption of shares keys that are used for signing consensus messages and duties. \
Note that an operator won't be functional in case the key was lost.

### Network Peers

There are three types of nodes in the network:

##### Operator

Operator node is responsible for signing validators duties. \
It holds relevant registry data and the validators consensus data.

##### Bootnode

Bootnode is a public peer which is responsible for helping new peers to find other peers in the network. \
The bootnode have a static (and stable) ENR so other peers could join the network easily.

##### Exporter

Exporter role is to allow to export information from the network. \
It collects registry data (validators / operators) and consensus data (decided messages chains) of all validators in the network.

### Messaging

Messages in the network are being transported p2p with one of the following methods:

#### Streams

Libp2p allows to create a bidirectional stream between two peers and implement a wire messaging protocol. \
See more information in [IPFS specs > communication-model - streams](https://ipfs.io/ipfs/QmVqNrDfr2dxzQUo4VN3zhG4NV78uYFmRpgSktWDc2eeh2/specs/7-properties/#71-communication-model---streams).

Streams are used in cases where message audience is a single peer.

#### PubSub

PubSub is used as an infrastructure for broadcasting messages among a group (AKA subnet) of operator nodes.

GossipSub ([v1.1](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md)) is the pubsub protocol used in SSV. \
In short, each node save metadata regards topic subscription of other peers in the network. \
With that information, messages are propagated to the most relevant peers (subscribed or neighbors of a subscribed peer) and therefore reduce the overall traffic.

## Protocols

Network interaction is achieved by using the following protocols:

### 1. Consensus

**TODO**
- IBFT/QBFT consensus
- state/decided propagation

#### Message

Messages in the network are formatted with `protobuf`. \
The basic message structure includes the following fields:

```protobuf
syntax = "proto3";

// Message represents the object that is being passed around the network
message Message {
  // type is the IBFT state / stage
  RoundState type   = 1;
  // round is the current round where the message was sent
  uint64 round      = 2;
  // lambda is the message identifier
  bytes lambda      = 3;
  // sequence number is an incremental number for each instance, much like a block number would be in a blockchain
  uint64 seq_number = 4;
  // value holds the message data in bytes
  bytes value       = 5;
}
```

IBFT stage is represented by an enum:

```protobuf
// RoundState is the available types of IBFT state / stage
enum RoundState {
  // NotStarted is when no instance has started yet
  NotStarted  = 0;
  // PrePrepare is the first stage in IBFT
  PrePrepare  = 1;
  // Prepare is the second stage in IBFT
  Prepare     = 2;
  // Commit is when an instance receives a qualified quorum of prepare msgs, then sends a commit msg
  Commit      = 3;
  // ChangeRound is sent upon round change
  ChangeRound = 4;
  // Decided is when an instance receives a qualified quorum of commit msgs
  Decided     = 5;
  // Stopped is the state of an instance that stopped running
  Stopped     = 6;
}
```

`SignedMessage` is the wrapping object that adds a signature and the corresponding singers:

```protobuf
syntax = "proto3";
import "gogo.proto";

// SignedMessage is a wrapper on top of Message for supporting signatures
message SignedMessage{
  // message is the raw message to sign
  Message message = 1 [(gogoproto.nullable) = false];
  // signature is a signature of the message
  bytes signature = 2 [(gogoproto.nullable) = false];
  // signer_ids are the IDs of the signing operators
  repeated uint64 signer_ids = 3;
}
```

**NOTE** that all pubsub messages in the network are wrapped by libp2p's message structure

#### Topics/Subnets

Messages in the network are being sent over a subnet/topic, which the relevant peers should be subscribed to. \
This helps to reduce the overall bandwidth, related resources etc.

There are several options for how setup topics in the network.

**NOTE** The first version of SSV testnet is using the first approach (topic per validator)

##### Topic per validator

Each validator has a dedicated pubsub topic with all the relevant peers subscribed to it.

It helps to reduce amount of messages in the network, but increases the number of topics.

##### Topic per multiple validators

The other option is to use a single topic for multiple validators, 
which helps to reduce to number of total topics but will cause a growth 
in the number of messages each peer is getting.

Topic list could be implemented in several ways:
1. fixed/static list - a fixed list of <x> (e.g. 128) topics
2. dynamic list - grows/shrinks according to given validators set


### 2. History Sync

History sync is the procedure of syncing decided messages from other peers. \
It is a prerequisite for taking part in some validator's consensus.

Sync is done over streams as pubsub is not suitable for this case due to several reasons such as:
- API nature is request/response, unlike broadcasting in consensus messages 
- Bandwidth - only one peer (usually) needs the data
- Adding complementary features like rate limiting will be easier to achieve 

#### Stream Protocols

The following protocols are used as part of history sync:

##### Heights Decided

`/sync/highest_decided/0.0.1`

**TODO**

##### Decided By Range 

`/sync/decided_by_range/0.0.1`

**TODO**

##### Last Change Round

`/sync/last_change_round/0.0.1`

**TODO**

## Networking

### Discovery

**TODO**

##### ENR

**TODO**

#### Alternatives

- Kademlia DHT (**TODO**)

### Forks

**TODO**

## Open points

* Authentication (correlation between network and operator keys)
* Heartbeat ?
