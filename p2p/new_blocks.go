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

	// очікування транзакцій для блоку
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
		To:        n.keys.Pub,
		Amount:    REWARD,
		Timestamp: time.Now().UnixMilli(),
		Nonce:     0,
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
		log.Error().Uint32("локальний блоку", lastLocalBlock.Height).Uint32("отриманий блоку", block.Height).Hex("hash отриманого блоку", block.Hash).Hex("hash локального блоку", lastLocalBlock.Hash).Msg("висота отриманого блоку і очікувана висота не збігаются")
		n.syncBlockchain()
		return fmt.Errorf("err")
	}
	// Перевірка підпису і hash`у
	if err := block.Verify(); err != nil {
		log.Error().Err(err).Hex("proposer", block.Proposer).Msg("валідація підпису блоку не пройшла")
		return fmt.Errorf("err")
	}
	// Перевірка підпису усіх транзакцій
	if err := block.VerifyTransactions(); err != nil {
		log.Error().Err(err).Msg("верефікаця транзакцій блоку не пройшла")
		return err
	}
	// Перевірка балансів і Nonce`ів усіх транзакцій
	if err := n.checkBalances(block.Transactions); err != nil {
		log.Error().Err(err).Msg("помилка перевірки бланасів/nonce транзакцій")
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
	// Перевірка транзакції нагороди
	if bytes.Equal(tx.From, []byte(REWARDWALLET)) {
		if tx.Amount != REWARD {
			return fmt.Errorf("транзакція нагороди має не правельну нагороду")
		}
		return nil
	}

	wallet, err := n.bs.GetWalletByAddress(tx.From)
	if err != nil {
		return fmt.Errorf("помилка отримання даних про гаманець: %w", err)
	}

	// Не вистачає балансу
	if wallet.Balance < tx.Amount {
		return fmt.Errorf("гаманець не має достатньої кількість грошей для переказу")
	}
	// Nonce не правельний
	if tx.Nonce != wallet.Nonce+1 {
		return fmt.Errorf("транзакція має не правельний Nonce: %d, коли Nonce гаманця це: %d", tx.Nonce, wallet.Nonce)
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
		// HACK: не найкраще рішення, через повторення логіки
		// HACK: якщо той, хто робить блок, відправить транзакцію то Nonce оновится 2 рази
		if bytes.Equal(tx.From, []byte(STAKE)) || bytes.Equal(tx.From, []byte(REWARDWALLET)) {
			walletTo, err := n.bs.GetWalletByAddress(tx.To)
			if err != nil {
				return err
			}
			walletTo.Balance += tx.Amount
			walletTo.Nonce++
			n.bs.UpdateBalance(&walletTo)
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

		// оновлюю баланси
		walletFrom.Balance -= tx.Amount
		walletTo.Balance += tx.Amount

		// додаю +1 до Nonce
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
