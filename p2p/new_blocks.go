package p2p

import (
	"bytes"
	"fmt"
	"sort"
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

	nextProposer, err := chain.SelectNextProposer(lastBlock.Hash, *validators, n.currentRound)
	if err != nil {
		return chain.Validator{}, err
	}

	return *nextProposer, nil
}

func (n *Node) createNewBlock() (chain.Block, error) {
	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		return chain.Block{}, fmt.Errorf("помилка отримання останнього блоку: %w", err)
	}
	
	expectedHeight := lastBlock.Height + 1
	expectedRound := n.currentRound

	log.Info().Uint32("height", expectedHeight).Uint32("round", expectedRound).Msg("очікування транзакцій для нового блоку")

	var txsToInclude []*chain.Transaction
	for {
		currentLastBlock, _ := n.bs.GetLastBlock()
		if currentLastBlock.Height >= expectedHeight || n.currentRound != expectedRound {
			return chain.Block{}, fmt.Errorf("стан змінився під час очікування транзакцій")
		}

		mempoolTXs := n.mempool.GetTransactions()
		var toDrop []*chain.Transaction
		txsToInclude, toDrop = n.getValidTransactions(mempoolTXs)

		if len(toDrop) > 0 {
			n.mempool.ClearMempool(toDrop)
		}

		if len(txsToInclude) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Info().Int("mempool", len(txsToInclude)).Msg("кількість транзакцій в mempool для нового блоку")

	block := chain.Block{
		Height:       lastBlock.Height + 1,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: txsToInclude,
	}

	if err = n.addRewardTx(&block); err != nil {
		return chain.Block{}, err
	}

	if err = block.Sign(n.keys.Priv); err != nil {
		return chain.Block{}, fmt.Errorf("помилка підпису блоку: %w", err)
	}

	if err = block.GenerateHash(); err != nil {
		return chain.Block{}, fmt.Errorf("помилка генерації хешу блоку: %w", err)
	}

	return block, nil
}

func (n *Node) addRewardTx(b *chain.Block) error {
	tx := chain.Transaction{
		From:      []byte(REWARDWALLET),
		To:        n.keys.Pub,
		Amount:    REWARD,
		Fee:       0,
		Timestamp: time.Now().UnixMilli(),
		Nonce:     0,
	}

	if err := tx.Sign(n.keys.Priv); err != nil {
		return fmt.Errorf("помилка підпису reward транзакції: %w", err)
	}

	b.Transactions = append(b.Transactions, &tx)
	return nil
}

func (n *Node) fullBlockVerefication(block *chain.Block) error {
	if !bytes.Equal(block.Proposer, n.nextProposer.Address) {
		log.Error().Hex("творець блоку", block.Proposer).Hex("хто повинен робити блок", n.nextProposer.Address).Msg("творець блоку і той, хто повинен робити блок, не збігаются")
		return fmt.Errorf("невірний proposer")
	}
	lastLocalBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання крайнього блоку з бази даних")
		return err
	}
	if lastLocalBlock.Height+1 != block.Height {
		log.Error().Uint32("локальний блок", lastLocalBlock.Height).Uint32("отриманий блок", block.Height).Hex("hash отриманого блоку", block.Hash).Msg("висота блоків не збігається")
		n.syncBlockchain()
		return fmt.Errorf("невірна висота блоку")
	}
	if err := block.Verify(); err != nil {
		log.Error().Err(err).Hex("proposer", block.Proposer).Msg("валідація підпису блоку не пройшла")
		return fmt.Errorf("невірний підпис блоку")
	}
	if err := block.VerifyTransactions(); err != nil {
		log.Error().Err(err).Msg("верифікація транзакцій блоку не пройшла")
		return err
	}
	if err := n.checkBalances(block.Transactions); err != nil {
		log.Error().Err(err).Msg("помилка перевірки балансів/nonce транзакцій")
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
	log.Debug().Hex("proposer", n.nextProposer.Address).Int64("баланс", n.nextProposer.Amount).Msg("наступний proposer")
	return nil
}

func (n *Node) isSystemAddr(addr []byte) bool {
	return bytes.Equal(addr, []byte(REWARDWALLET)) || bytes.Equal(addr, []byte(STAKE))
}

func (n *Node) getValidTransactions(txs []*chain.Transaction) ([]*chain.Transaction, []*chain.Transaction) {
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].Nonce < txs[j].Nonce
	})

	toInclude := make([]*chain.Transaction, 0, len(txs))
	toDrop := make([]*chain.Transaction, 0)
	
	nonces := make(map[string]uint32)
	balances := make(map[string]int64)

	for _, tx := range txs {
		if n.isSystemAddr(tx.From) {
			toInclude = append(toInclude, tx)
			continue
		}

		fromAddr := string(tx.From)
		if _, ok := nonces[fromAddr]; !ok {
			wallet, err := n.bs.GetWalletByAddress(tx.From)
			if err != nil {
				continue
			}
			nonces[fromAddr] = wallet.Nonce
			balances[fromAddr] = wallet.Balance
		}

		currentNonce := nonces[fromAddr]
		currentBalance := balances[fromAddr]

		if tx.Nonce <= currentNonce {
			toDrop = append(toDrop, tx)
			continue
		}

		if tx.Nonce > currentNonce+1 {
			continue
		}

		totalCost := tx.Amount + tx.Fee
		if currentBalance < totalCost {
			toDrop = append(toDrop, tx)
			continue
		}

		toInclude = append(toInclude, tx)
		nonces[fromAddr] = tx.Nonce
		balances[fromAddr] = currentBalance - totalCost
	}
	return toInclude, toDrop
}

func (n *Node) checkBalances(txs []*chain.Transaction) error {
	nonces := make(map[string]uint32)
	balances := make(map[string]int64)

	for _, tx := range txs {
		if n.isSystemAddr(tx.From) {
			continue
		}

		fromAddr := string(tx.From)
		if _, ok := nonces[fromAddr]; !ok {
			wallet, err := n.bs.GetWalletByAddress(tx.From)
			if err != nil {
				return err
			}
			nonces[fromAddr] = wallet.Nonce
			balances[fromAddr] = wallet.Balance
		}

		currentNonce := nonces[fromAddr]
		currentBalance := balances[fromAddr]

		if tx.Nonce != currentNonce+1 {
			return fmt.Errorf("невірний Nonce транзакції: %d, очікується %d (гаманець: %x)", tx.Nonce, currentNonce+1, tx.From)
		}

		totalCost := tx.Amount + tx.Fee
		if currentBalance < totalCost {
			return fmt.Errorf("недостатній баланс: %d < %d", currentBalance, totalCost)
		}

		nonces[fromAddr] = tx.Nonce
		balances[fromAddr] = currentBalance - totalCost
	}
	return nil
}

func (n *Node) updateBalancesNonces(b *chain.Block) error {
	for _, tx := range b.Transactions {
		if n.isSystemAddr(tx.From) {
			walletTo, err := n.bs.GetWalletByAddress(tx.To)
			if err != nil {
				return err
			}
			walletTo.Balance += tx.Amount
			if err = n.bs.UpdateBalance(&walletTo); err != nil {
				return fmt.Errorf("помилка оновлення балансу гаманця %x: %w", walletTo.Address, err)
			}
			continue
		}
		walletFrom, err := n.bs.GetWalletByAddress(tx.From)
		if err != nil {
			return err
		}
		walletTo, err := n.bs.GetWalletByAddress(tx.To)
		if err != nil {
			return err
		}

		walletFrom.Balance -= (tx.Amount + tx.Fee)
		walletTo.Balance += tx.Amount
		walletFrom.Nonce++

		if err := n.bs.UpdateBalance(&walletFrom); err != nil {
			return err
		}
		if err := n.bs.UpdateBalance(&walletTo); err != nil {
			return err
		}

		if tx.Fee > 0 {
			rewardWallet, err := n.bs.GetWalletByAddress([]byte(REWARDWALLET))
			if err != nil {
				return err
			}
			rewardWallet.Balance += tx.Fee
			if err := n.bs.UpdateBalance(&rewardWallet); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *Node) addValidatorsToDB(block *chain.Block) error {
	for _, tx := range block.Transactions {
		if bytes.Equal(tx.To, []byte(STAKE)) {
			validator, _ := n.bs.GetValidator(tx.From)
			if validator != nil {
				log.Info().Int64("був", validator.Amount).Int64("став", validator.Amount+tx.Amount).Msg("оновлено баланс валідатора")
				validator.Amount += tx.Amount
			} else {
				validator = &chain.Validator{
					Address: tx.From,
					Amount:  tx.Amount,
				}
				log.Info().Int64("amount", validator.Amount).Msg("додано валідатора")
			}

			if err := n.bs.AddValidator(validator); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *Node) deleteValidatorsFromDB(block *chain.Block) error {
	for _, tx := range block.Transactions {
		if bytes.Equal(tx.To, []byte("unstake")) {
			validator := &chain.Validator{
				Address: tx.From,
				Amount:  tx.Amount,
			}
			if err := n.bs.DeleteValidator(validator); err != nil {
				return err
			}
		}
	}
	return nil
}
