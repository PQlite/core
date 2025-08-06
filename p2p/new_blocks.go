package p2p

import (
	"fmt"
	"time"

	"github.com/PQlite/core/chain"
)

func (n *Node) chooseValidator() (chain.Validator, error) {
	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		return chain.Validator{}, fmt.Errorf("помилка отримання останнього блоку: %w", err)
	}
	validators, err := n.bs.GetValidatorsList()
	if err != nil {
		return chain.Validator{}, fmt.Errorf("помилка отримання списку валідаторів: %w", err)
	}

	nextProposer, err := chain.SelectNextProposer(lastBlock.Hash, *validators)
	if err != nil {
		return chain.Validator{}, err
	}

	return *nextProposer, nil
}

func (n *Node) createNewBlock() chain.Block {
	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		// NOTE: може не найкращів варіант
		panic(err)
	}

	for len(n.mempool.TXs) < 1 {
		time.Sleep(100 * time.Millisecond)
	}

	ublock := chain.BlockForSign{
		Height:       lastBlock.Height + 1,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: n.mempool.TXs,
	}

	block, err := ublock.Sign(n.keys.Priv)
	if err != nil {
		panic(err)
	}

	n.addRewardTx(&block)

	n.mempool.TXs = nil // очищення TXs

	return block
}

func (n *Node) addRewardTx(b *chain.Block) {
	tx := chain.Transaction{
		From:      []byte(REWARDWALLET),
		To:        b.Proposer,
		Amount:    REWARD,
		Timestamp: time.Now().UnixMilli(),
		Nonce:     1, // FIXME: зробити нормально
		PubKey:    n.keys.Pub,
	}

	err := tx.Sign(n.keys.Priv)
	if err != nil {
		panic(err)
	}

	b.Transactions = append(b.Transactions, &tx)
}
