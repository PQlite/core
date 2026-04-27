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

	log.Info().Msg("очікування транзакцій для нового блоку")

	for {
		n.mempool.TXs = n.getOnlyValidTransaction(n.mempool.TXs)
		if n.mempool.Len() > 0 {
			break
		}
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

func (n *Node) validateTx(tx *chain.Transaction) error {
	if bytes.Equal(tx.From, []byte(REWARDWALLET)) {
		if tx.Amount != REWARD {
			return fmt.Errorf("транзакція нагороди має неправильну суму")
		}
		return nil
	}

	wallet, err := n.bs.GetWalletByAddress(tx.From)
	if err != nil {
		return fmt.Errorf("помилка отримання даних про гаманець: %w", err)
	}

	if wallet.Balance < tx.Amount {
		return fmt.Errorf("недостатній баланс для переказу")
	}
	if tx.Nonce != wallet.Nonce+1 {
		return fmt.Errorf("невірний Nonce транзакції: %d, Nonce гаманця: %d", tx.Nonce, wallet.Nonce)
	}

	return nil
}

func (n *Node) getOnlyValidTransaction(txs []*chain.Transaction) []*chain.Transaction {
	validTxs := make([]*chain.Transaction, 0, len(txs))
	for _, tx := range txs {
		if err := n.validateTx(tx); err == nil {
			validTxs = append(validTxs, tx)
		}
	}
	return validTxs
}

func (n *Node) checkBalances(txs []*chain.Transaction) error {
	for _, tx := range txs {
		if err := n.validateTx(tx); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) updateBalancesNonces(b *chain.Block) error {
	for _, tx := range b.Transactions {
		// HACK: якщо proposer надсилає звичайну tx, то його Nonce оновиться двічі
		if bytes.Equal(tx.From, []byte(STAKE)) || bytes.Equal(tx.From, []byte(REWARDWALLET)) {
			walletTo, err := n.bs.GetWalletByAddress(tx.To)
			if err != nil {
				return err
			}
			walletTo.Balance += tx.Amount
			// Nonce не збільшуємо — він відслідковує лише відправлені (outgoing) транзакції
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

		walletFrom.Balance -= tx.Amount
		walletTo.Balance += tx.Amount
		walletFrom.Nonce++

		if err := n.bs.UpdateBalance(&walletFrom); err != nil {
			return err
		}
		if err := n.bs.UpdateBalance(&walletTo); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) addValidatorsToDB(block *chain.Block) error {
	// ISSUE: треба додавати баланс до валідатора, якщо він вже існує, а не перезаписувати його
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
		if bytes.Equal(tx.From, []byte("unstake")) {
			validator, err := n.bs.GetValidator(tx.To)
			if err != nil {
				log.Error().Err(err).Msg("помилка отримання валідатора при unstake")
				return err
			}

			if err = n.bs.DeleteValidator(validator); err != nil {
				log.Error().Err(err).Msg("помилка видалення валідатора при unstake")
				return err
			}
		}
	}
	return nil
}
