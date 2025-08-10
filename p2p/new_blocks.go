package p2p

import (
	"fmt"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/rs/zerolog/log"
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
		log.Fatal().Err(err).Msg("помилка отримання останнього блоку")
	}

	log.Info().Msg("очікування транзакцій для нового блоку")
	for n.mempool.Len() < 1 {
		time.Sleep(1 * time.Second)
	}

	ublock := chain.BlockForSign{
		Height:       lastBlock.Height + 1,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: n.mempool.TXs,
	}

	n.addRewardTx(&ublock)

	block, err := ublock.Sign(n.keys.Priv)
	if err != nil {
		log.Fatal().Err(err).Msg("помилка підпису блоку")
	}

	n.mempool.TXs = nil // очищення TXs

	return block
}

func (n *Node) addRewardTx(b *chain.BlockForSign) {
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
		log.Fatal().Err(err).Msg("помилка підпису транзакції")
	}

	b.Transactions = append(b.Transactions, &tx)
}
