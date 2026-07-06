package raft

import "errors"

type InMemoryTransport struct {
	NodesByID map[string]*Raft
}

func (t InMemoryTransport) RequestVote(peerID string, req VoteRequest) (VoteResponse, error) {
	node, ok := t.NodesByID[peerID]
	if !ok {
		return VoteResponse{}, errors.New("node not found")
	}
	return node.HandleRequestVote(req), nil
}

func (t InMemoryTransport) AppendEntries(peerID string, req AppendRequest) (AppendResponse, error) {
	node, ok := t.NodesByID[peerID]
	if !ok {
		return AppendResponse{}, errors.New("node not found")
	}
	return node.HandleAppendEntries(req), nil

}
