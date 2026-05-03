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
	// Витримуємо мінімальний час між блоками
	timeSinceLastBlock := time.Since(n.lastBlockTime)
	if timeSinceLastBlock < MinBlockTime {
		sleepTime := MinBlockTime - timeSinceLastBlock
		log.Debug().Dur("sleep", sleepTime).Msg("затримка перед створенням наступного блоку")
		time.Sleep(sleepTime)
	}

	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		return chain.Block{}, fmt.Errorf("помилка отримання останнього блоку: %w", err)
	}
	
	expectedHeight := lastBlock.Height + 1
	expectedRound := n.currentRound

	log.Info().Uint32("height", expectedHeight).Uint32("round", expectedRound).Msg("початок створення блоку")

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

		// Якщо порожні блоки дозволені АБО у нас є хоча б одна транзакція — виходимо з циклу
		if AllowEmptyBlocks || len(txsToInclude) > 0 {
			break
		}

		// Якщо транзакцій немає і порожні блоки заборонені — чекаємо
		time.Sleep(100 * time.Millisecond)
	}

	log.Info().Int("mempool", len(txsToInclude)).Msg("кількість транзакцій в mempool для нового блоку")

	block := chain.Block{
		Height:       lastBlock.Height + 1,
		Round:        n.currentRound,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: txsToInclude,
	}

	if err = n.addRewardTx(&block); err != nil {
		return chain.Block{}, err
	}

	if err = n.addPenaltyTxs(&block); err != nil {
		return chain.Block{}, err
	}

	if err = block.Sign(n.keys.Priv); err != nil {
		return chain.Block{}, fmt.Errorf("помилка підпису блоку: %w", err)
	}

	if err = block.GenerateHash(); err != nil {
		return chain.Block{}, fmt.Errorf("помилка генерації хешу блоку: %w", err)
	}

	n.lastBlockTime = time.Now() // Оновлюємо час останнього блоку
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

func (n *Node) addPenaltyTxs(b *chain.Block) error {
	if b.Round == 0 {
		return nil
	}

	lastBlock, err := n.bs.GetBlock(b.Height - 1)
	if err != nil {
		return fmt.Errorf("помилка отримання попереднього блоку для штрафів: %w", err)
	}

	validators, err := n.bs.GetValidatorsList()
	if err != nil {
		return fmt.Errorf("помилка отримання списку валідаторів для штрафів: %w", err)
	}

	for r := uint32(0); r < b.Round; r++ {
		missedProposer, err := chain.SelectNextProposer(lastBlock.Hash, *validators, r)
		if err != nil {
			continue
		}

		// Якщо нода пропустила раунд, але зробила блок у наступному — не штрафуємо її (вимога п.1)
		if bytes.Equal(missedProposer.Address, b.Proposer) {
			continue
		}

		// Штраф: 1% від стейку
		penalty := missedProposer.Amount / 100
		if penalty == 0 && missedProposer.Amount > 0 {
			penalty = 1
		}

		if penalty > 0 {
			tx := chain.Transaction{
				From:      missedProposer.Address,
				To:        []byte(FINEWALLET),
				Amount:    penalty,
				Fee:       0,
				Timestamp: time.Now().UnixMilli(),
				Nonce:     0, // Системні транзакції можуть мати 0 або спеціальний nonce
			}
			// Підписуємо ключем нашої ноди, бо це ми створюємо блок (хоча для fine ми в Verify зробили виняток)
			if err := tx.Sign(n.keys.Priv); err != nil {
				return fmt.Errorf("помилка підпису penalty транзакції: %w", err)
			}
			b.Transactions = append(b.Transactions, &tx)
		}
	}
	return nil
}

func (n *Node) applyPenalties(block *chain.Block) error {
	for _, tx := range block.Transactions {
		if bytes.Equal(tx.To, []byte(FINEWALLET)) {
			validator, _ := n.bs.GetValidator(tx.From)
			if validator != nil {
				log.Warn().
					Hex("proposer", validator.Address).
					Uint32("height", block.Height).
					Int64("penalty", tx.Amount).
					Msg("застосування штрафу з транзакції блоку")

				validator.Amount -= tx.Amount
				if validator.Amount < 0 {
					validator.Amount = 0
				}

				if err := n.bs.AddValidator(validator); err != nil {
					return fmt.Errorf("помилка оновлення валідатора після штрафу: %w", err)
				}
			}
		}
	}
	return nil
}

func (n *Node) fullBlockVerefication(block *chain.Block) error {
	lastLocalBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання крайнього блоку з бази даних")
		return err
	}

	// Якщо ми отримали блок, який уже маємо (або старіший), просто ігноруємо його без помилки
	if block.Height <= lastLocalBlock.Height {
		return fmt.Errorf("блок уже оброблений (height %d <= %d)", block.Height, lastLocalBlock.Height)
	}

	// Якщо висота занадто велика — запускаємо синхронізацію, але не шлемо reject (можливо ми просто відстали)
	if lastLocalBlock.Height+1 != block.Height {
		log.Warn().Uint32("local", lastLocalBlock.Height).Uint32("received", block.Height).Msg("отримано блок з майбутнього, запускаємо синхронізацію")
		go n.syncBlockchain()
		return fmt.Errorf("невірна висота блоку")
	}

	// Перевірка timestamp блоку
	if block.Timestamp <= lastLocalBlock.Timestamp {
		return fmt.Errorf("блок має застарілий timestamp (%d <= %d)", block.Timestamp, lastLocalBlock.Timestamp)
	}
	// Дозволяємо невелике відхилення в майбутнє (наприклад, 10 секунд) для синхронізації годинників
	if block.Timestamp > time.Now().Add(10*time.Second).UnixMilli() {
		return fmt.Errorf("блок має timestamp з майбутнього (%d)", block.Timestamp)
	}

	// ВАЖЛИВО: Спочатку перевіряємо підпис самого блоку та його структуру
	if err := block.Verify(); err != nil {
		log.Error().Err(err).Hex("proposer", block.Proposer).Msg("валідація підпису блоку не пройшла")
		return fmt.Errorf("невірний підпис блоку")
	}

	// ВАЖЛИВО: Перевіряємо чи proposer блоку відповідає очікуваному для цього раунду
	validators, err := n.bs.GetValidatorsList()
	if err != nil {
		return fmt.Errorf("помилка отримання списку валідаторів: %w", err)
	}
	expectedProposer, err := chain.SelectNextProposer(lastLocalBlock.Hash, *validators, block.Round)
	if err != nil {
		return fmt.Errorf("помилка вибору очікуваного proposer-а: %w", err)
	}

	if !bytes.Equal(block.Proposer, expectedProposer.Address) {
		return fmt.Errorf("невірний proposer для висоти %d раунду %d", block.Height, block.Round)
	}

	// Якщо раунд блоку відрізняється від нашого — оновлюємо свій стан
	if block.Round != n.currentRound {
		log.Warn().
			Uint32("local_round", n.currentRound).
			Uint32("block_round", block.Round).
			Msg("раунд блоку відрізняється від локального — синхронізація раунду")
		n.currentRound = block.Round
	}

	if err := block.VerifyTransactions(); err != nil {
		log.Error().Err(err).Msg("верифікація транзакцій блоку не пройшла")
		return err
	}

	if err := n.checkBalances(block.Transactions); err != nil {
		log.Error().Err(err).Msg("помилка перевірки балансів/nonce транзакцій")
		return err
	}

	if err := n.verifyPenaltyTxs(block); err != nil {
		log.Error().Err(err).Msg("невідповідність штрафних транзакцій у блоці")
		return err
	}

	return nil
}

func (n *Node) verifyPenaltyTxs(b *chain.Block) error {
	if b.Round == 0 {
		// Якщо раунд 0, штрафів бути не повинно
		for _, tx := range b.Transactions {
			if bytes.Equal(tx.To, []byte(FINEWALLET)) {
				return fmt.Errorf("штрафна транзакція в блоці раунду 0")
			}
		}
		return nil
	}

	lastBlock, err := n.bs.GetBlock(b.Height - 1)
	if err != nil {
		return fmt.Errorf("помилка отримання попереднього блоку: %w", err)
	}

	validators, err := n.bs.GetValidatorsList()
	if err != nil {
		return fmt.Errorf("помилка отримання списку валідаторів: %w", err)
	}

	// Створюємо карту очікуваних штрафів
	expectedFines := make(map[string]int64)
	for r := uint32(0); r < b.Round; r++ {
		missedProposer, err := chain.SelectNextProposer(lastBlock.Hash, *validators, r)
		if err != nil {
			continue
		}

		// Якщо нода пропустила раунд, але зробила блок у наступному — не штрафуємо її (вимога п.1)
		if bytes.Equal(missedProposer.Address, b.Proposer) {
			continue
		}

		// Штраф: 1% від стейку
		penalty := missedProposer.Amount / 100
		if penalty == 0 && missedProposer.Amount > 0 {
			penalty = 1
		}
		if penalty > 0 {
			expectedFines[string(missedProposer.Address)] += penalty
		}
	}

	// Створюємо карту отриманих штрафів у блоці
	actualFines := make(map[string]int64)
	for _, tx := range b.Transactions {
		if bytes.Equal(tx.To, []byte(FINEWALLET)) {
			actualFines[string(tx.From)] += tx.Amount
		}
	}

	// Порівнюємо
	if len(expectedFines) != len(actualFines) {
		return fmt.Errorf("невідповідність кількості штрафованих адрес: очікувано %d, отримано %d", len(expectedFines), len(actualFines))
	}

	for addr, amount := range expectedFines {
		if actualFines[addr] != amount {
			return fmt.Errorf("невірна сума штрафу для %x: очікувано %d, отримано %d", addr, amount, actualFines[addr])
		}
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

// ResetRound скидає консенсус для нової висоти
func (n *Node) ResetRound() {
	n.currentRound = 0
	if err := n.setNextProposer(); err != nil {
		log.Error().Err(err).Msg("не вдалося оновити proposer при ResetRound")
	}
	
	// Скидаємо прапорець, щоб дозволити нову пропозицію негайно (якщо попередня зависла)
	n.isProposing.Store(false)

	// Якщо ми наступний proposer — пробуємо запропонувати блок
	if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		go n.tryProposeBlock()
	}
}

func (n *Node) isSystemAddr(addr []byte) bool {
	return bytes.Equal(addr, []byte(REWARDWALLET)) || bytes.Equal(addr, []byte(STAKE)) || bytes.Equal(addr, []byte(FINEWALLET)) || bytes.Equal(addr, []byte(UNSTAKE))
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
		if n.isSystemAddr(tx.From) || bytes.Equal(tx.To, []byte(FINEWALLET)) {
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

		// Штрафні транзакції не зменшують баланс гаманця, бо вони зменшують стейк у applyPenalties
		if bytes.Equal(tx.To, []byte(FINEWALLET)) {
			walletTo, _ := n.bs.GetWalletByAddress(tx.To)
			walletTo.Balance += tx.Amount
			n.bs.UpdateBalance(&walletTo)
			continue
		}

		// При Unstake гроші повертаються з системи (стейку) на баланс гаманця
		if bytes.Equal(tx.To, []byte(UNSTAKE)) {
			walletFrom, err := n.bs.GetWalletByAddress(tx.From)
			if err != nil {
				return err
			}
			walletFrom.Balance += tx.Amount
			walletFrom.Balance -= tx.Fee
			walletFrom.Nonce++
			if err = n.bs.UpdateBalance(&walletFrom); err != nil {
				return err
			}

			// Комісія йде в reward
			if tx.Fee > 0 {
				rewardWallet, _ := n.bs.GetWalletByAddress([]byte(REWARDWALLET))
				rewardWallet.Balance += tx.Fee
				n.bs.UpdateBalance(&rewardWallet)
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
				log.Debug().Int64("був", validator.Amount).Int64("став", validator.Amount+tx.Amount).Msg("оновлено баланс валідатора")
				validator.Amount += tx.Amount
			} else {
				validator = &chain.Validator{
					Address: tx.From,
					Amount:  tx.Amount,
				}
				log.Debug().Int64("amount", validator.Amount).Msg("додано валідатора")
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
		if bytes.Equal(tx.To, []byte(UNSTAKE)) {
			validator, _ := n.bs.GetValidator(tx.From)
			if validator == nil {
				continue
			}

			validator.Amount -= tx.Amount
			if validator.Amount <= 0 {
				if err := n.bs.DeleteValidator(validator); err != nil {
					return err
				}
				log.Debug().Hex("address", validator.Address).Msg("валідатора видалено (повний unstake)")
			} else {
				if err := n.bs.AddValidator(validator); err != nil {
					return err
				}
				log.Debug().Hex("address", validator.Address).Int64("залишок", validator.Amount).Msg("оновлено стейк валідатора (частковий unstake)")
			}
		}
	}
	return nil
}
