package p2p

import (
	"bytes"
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
		time.Sleep(100 * time.Millisecond)
	}

	log.Info().Int("mempool", n.mempool.Len()).Msg("кількість транзакцій в mempool")

	block := chain.Block{
		Height:       lastBlock.Height + 1,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: n.mempool.TXs,
	}

	n.addRewardTx(&block)

	if err = block.Sign(n.keys.Priv); err != nil {
		log.Fatal().Err(err).Msg("помилка підпису блоку")
	}

	block.GenerateHash()

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
		log.Fatal().Err(err).Msg("помилка підпису транзакції")
	}

	b.Transactions = append(b.Transactions, &tx)
}

func (n *Node) fullBlockVerefication(block *chain.Block) error {
	// Чи правельний творець блоку
	if !bytes.Equal(block.Proposer, n.nextProposer.Address) {
		log.Error().Hex("творець блоку", block.Proposer).Hex("хто повинен робити блок", n.nextProposer.Address).Msg("творець блоку і той, хто повинен робити блок, не збігаются")
		return fmt.Errorf("err")
	}
	// чи правельна висота блоку який був отриманий (на один більше попереднього)
	lastLocalBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання крайнього блоку з бази даних")
		return err
	}
	if lastLocalBlock.Height+1 != block.Height {
		log.Error().Uint32("локальний блоку", lastLocalBlock.Height).Uint32("отриманий блоку", block.Height).Msg("висота отриманого блоку і очікувана висота не збігаются")
		n.syncBlockchain()
		return fmt.Errorf("err")
	}
	// Перевірка підпису і hash`у
	if err := block.Verify(); err != nil {
		log.Error().Err(err).Msg("валідація підпису блоку не пройшла")
		return fmt.Errorf("err")
	}
	// Перевірка підпису і балансів усіх транзакцій в тому чеслі перевірку нагороди для валідатора
	if err := block.VerifyTransactions(); err != nil {
		log.Error().Err(err).Msg("верефікаця транзакцій блоку не пройшла")
		return err
	}
	return nil
}

func (n *Node) setNextProposer() error {
	nextProposer, err := n.chooseValidator()
	if err != nil {
		log.Error().Err(err).Msg("помилка вибору наступного валідатора")
		return err
	}
	n.nextProposer = nextProposer
	return nil
}

func (n *Node) addValidatorsToDB(block *chain.Block) error {
	// ISSUE: треба додавати баланс до валідатора, якщо він вже існує, а не перезаписувати його
	for _, tx := range block.Transactions {
		if bytes.Equal(tx.To, []byte(STAKE)) {
			validator := chain.Validator{
				Address: tx.PubKey,
				Amount:  tx.Amount,
			}

			if err := n.bs.AddValidator(&validator); err != nil {
				return err
			}

			log.Info().Float32("amount", validator.Amount).Msg("додано валідатора")
		}
	}
	return nil
}
