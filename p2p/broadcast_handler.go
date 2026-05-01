package p2p

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

// proposalTimeout — скільки очікуємо блок від proposer-а перед тим як перейти до наступного раунду.
// Якщо proposer не відповів або зробив поганий блок — всі ноди збільшують раунд і
// SelectNextProposer вибирає іншого кандидата для тієї ж висоти.
const proposalTimeout = 15 * time.Second

func (n *Node) handleBroadcastMessages() {
	go n.processBlockProposalCommit()
	for {
		data, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Error().Err(err).Msg("помилка при отриманні повідомлення")
			continue
		}

		var message Message
		if err = json.Unmarshal(data.Data, &message); err != nil {
			log.Error().Err(err).Msg("помилка розпаковки повідомлення")
			continue
		}

		if data.ReceivedFrom == n.host.ID() {
			if message.Type != MsgBlockProposal && message.Type != MsgCommit && message.Type != MsgVote && message.Type != MsgReject {
				log.Debug().Msg("повідомлення від себе")
				continue
			}
		}

		if !message.verify() {
			log.Warn().Msg("підпис повідомлення not valid")
			continue
		}

		log.Debug().Str("type", string(message.Type)).Msg("отримав повідомлення")

		switch message.Type {
		case MsgNewTransaction:
			go n.handleMsgNewTransaction(message.Data)
		case MsgVote:
			go n.handleMsgVote(message.Data)
		default:
			n.messagesQueue <- message
		}
	}
}

// processBlockProposalCommit обробляє консенсусні повідомлення послідовно.
// Таймер запускається лише після першого отриманого блоку чи коміту (після синхронізації),
// тому не спрацьовує передчасно під час завантаження блоків.
func (n *Node) processBlockProposalCommit() {
	var timer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case message := <-n.messagesQueue:
			switch message.Type {
			case MsgBlockProposal:
				timer, timerCh = restartTimer(timer, proposalTimeout)
				n.handleMsgBlockProposal(message.Data)
			case MsgCommit:
				timer, timerCh = restartTimer(timer, proposalTimeout)
				n.handleMsgCommit(message.Data)
			case MsgReject:
				n.handleMsgReject(message.Data)
			}

		case <-timerCh:
			// Proposer не надіслав блок вчасно.
			// Якщо ця нода сама є proposer-ом — вона просто ще не отримала транзакцій,
			// тому пропускаємо таймаут щоб не відхиляти себе.
			if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
				timer, timerCh = restartTimer(timer, proposalTimeout)
				continue
			}
			log.Warn().
				Hex("proposer", n.nextProposer.Address).
				Uint32("round", n.currentRound).
				Msg("timeout: proposer не відповів — переходимо до наступного раунду")
			n.advanceRound()
			timer, timerCh = restartTimer(timer, proposalTimeout)
		}
	}
}

func (n *Node) handleMsgNewTransaction(data []byte) {
	var tx chain.Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки транзакції")
		return
	}
	now := time.Now().UnixMilli()
	// Перевірка timestamp транзакції
	if tx.Timestamp > now+60000 { // +1 хвилина
		log.Warn().Msg("відхилено: транзакція з майбутнього")
		return
	}
	if tx.Timestamp < now-86400000 { // -24 години
		log.Warn().Msg("відхилено: транзакція занадто стара")
		return
	}

	log.Info().Int64("latency", now-tx.Timestamp).Msg("отримано транзакцію")
	
	// Захист від Mempool DOS: перевіряємо чи Nonce не занадто далеко в майбутньому
	wallet, _ := n.bs.GetWalletByAddress(tx.From)
	if tx.Nonce > wallet.Nonce+10 {
		log.Warn().Uint32("tx_nonce", tx.Nonce).Uint32("wallet_nonce", wallet.Nonce).Msg("відхилено: Nonce занадто далеко в майбутньому (DOS protection)")
		return
	}

	if err := n.mempool.Add(&tx); err != nil {
		log.Warn().Err(err).Msg("отримана транзакція не була додана до mempool")
	} else {
		if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
			go n.tryProposeBlock()
		}
	}
}

func (n *Node) handleMsgBlockProposal(data []byte) {
	var block chain.Block
	if err := json.Unmarshal(data, &block); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки blockProposal")
		return
	}
	log.Info().Uint32("height", block.Height).Int64("latency", time.Now().UnixMilli()-block.Timestamp).Msg("отримано новий блок")

	blockBytes, err := block.MarshalDeterministic()
	if err != nil {
		log.Error().Err(err).Msg("помилка перетворення блоку на []byte")
		return
	}

	if err := n.fullBlockVerefication(&block); err != nil {
		// Якщо блок уже оброблений — просто ігноруємо, це не помилка
		if strings.Contains(err.Error(), "оброблений") {
			log.Debug().Uint32("height", block.Height).Msg("отримано дублікат блоку, ігноруємо")
			return
		}
		
		// Якщо висота занадто велика — можливо ми відстали
		if strings.Contains(err.Error(), "висота") {
			log.Debug().Err(err).Msg("блок з іншою висотою, ігноруємо (запущено синхронізацію)")
			return
		}

		log.Warn().Err(err).Msg("блок не пройшов верифікацію — надсилаємо reject")
		n.rejectCurrentProposer()
		return
	}

	voteMsg, err := n.getVoteMsg(blockBytes)
	if err != nil {
		log.Error().Err(err).Msg("помилка створення повідомлення для голосування")
		return
	}
	if err = n.topic.broadcast(voteMsg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка розсилання голосу")
		return
	}

	// якщо це не я роблю блок — далі не йдемо
	if !bytes.Equal(block.Proposer, n.keys.Pub) {
		return
	}

	allValidators, err := n.bs.GetValidatorsList()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання списку валідаторів")
		return
	}

	var stakeAmount int64
	for _, v := range *allValidators {
		stakeAmount += v.Amount
	}

	var votersList []chain.Vote
	var acceptedAmount int64

	voteTimeout := time.NewTimer(30 * time.Second)
	defer voteTimeout.Stop()

collectVotes:
	for {
		select {
		case v := <-n.vote:
			if err = crypto.Verify(v.Pub, blockBytes, v.Signature); err != nil {
				log.Info().Msg("голос не є валідним")
				continue
			}
			contains, validator := containsInValidators(v.Pub, allValidators)
			if contains {
				acceptedAmount += validator.Amount
				votersList = append(votersList, v)
			}
			if (stakeAmount / 2) < acceptedAmount {
				break collectVotes
			}
		case <-voteTimeout.C:
			log.Warn().
				Int64("зібрано", acceptedAmount).
				Int64("потрібно", stakeAmount/2+1).
				Msg("timeout голосування — недостатньо голосів")
			return
		}
	}

	commitMsg, err := n.getCommitMsg(&votersList, &block)
	if err != nil {
		log.Error().Err(err).Msg("помилка створення commit повідомлення")
		return
	}
	if err = n.topic.broadcast(commitMsg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка відправки commit повідомлення")
		return
	}
	log.Debug().Msg("повідомлення commit відправлено")
}

func (n *Node) handleMsgVote(data []byte) {
	var vote chain.Vote
	if err := json.Unmarshal(data, &vote); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки vote повідомлення")
		return
	}
	log.Debug().Hex("від", vote.Pub).Msg("отримано повідомлення vote")
	select {
	case n.vote <- vote:
	default:
		log.Warn().Msg("vote channel повний, голос відкинуто")
	}
}

func (n *Node) handleMsgCommit(data []byte) {
	go drainChannel(n.vote)

	var commit Commit
	if err := json.Unmarshal(data, &commit); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки commit повідомлення")
		return
	}

	allValidators, err := n.bs.GetValidatorsList()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання списку валідаторів")
		return
	}

	var totalStake int64
	for _, v := range *allValidators {
		totalStake += v.Amount
	}

	var acceptedStake int64
	for _, v := range commit.Voters {
		if err := v.Verify(&commit.Block); err != nil {
			log.Error().Hex("voter", v.Pub).Msg("помилка підтвердження підпису голосу")
			return
		}
		contains, validator := containsInValidators(v.Pub, allValidators)
		if !contains {
			log.Error().Hex("voter", v.Pub).Msg("голос не був в списку валідаторів")
			return
		}
		acceptedStake += validator.Amount
	}

	if (totalStake / 2) >= acceptedStake {
		log.Error().
			Int64("зібрано", acceptedStake).
			Int64("потрібно", totalStake/2+1).
			Msg("commit не має достатньої кількості голосів")
		return
	}

	// Перевіряємо, чи блок іде точно наступним
	lastBlock, err := n.bs.GetLastBlock()
	if err == nil && commit.Block.Height != lastBlock.Height+1 {
		log.Error().Uint32("last_height", lastBlock.Height).Uint32("block_height", commit.Block.Height).Msg("спроба додати блок не за порядком")
		return
	}

	if err := n.bs.SaveBlock(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка збереження блоку")
		return
	}
	n.lastBlockTime = time.Now()
	log.Info().Hex("hash", commit.Block.Hash).Uint32("height", commit.Block.Height).Msg("новий блок додано до ланцюжка")

	if err := n.applyPenalties(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка застосування штрафів")
	}

	go n.mempool.ClearMempool(commit.Block.Transactions)

	if err := n.addValidatorsToDB(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка додавання валідаторів до БД")
		return
	}
	if err := n.deleteValidatorsFromDB(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка видалення валідаторів з БД")
		return
	}
	if err := n.updateBalancesNonces(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка оновлення балансів/nonce")
		return
	}

	// Новий блок — скидаємо раунд і вибираємо наступного proposer-а
	n.currentRound = 0
	if err := n.setNextProposer(); err != nil {
		log.Error().Err(err).Msg("помилка вибору наступного proposer")
		return
	}

	if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		go n.tryProposeBlock()
	}
}

func (n *Node) tryProposeBlock() {
	// Подвійна перевірка: чи ми все ще є proposer-ом?
	if !bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		return
	}

	if !n.isProposing.CompareAndSwap(false, true) {
		return
	}
	defer n.isProposing.Store(false)

	log.Info().Msg("я proposer — починаю створення блоку")

	blockProposalMsg, err := n.getMsgBlockProposalMsg()
	if err != nil {
		log.Debug().Err(err).Msg("не вдалося створити блок (можливо, стан змінився)")
		return
	}
	if err = n.topic.broadcast(blockProposalMsg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка трансляції нового блоку")
	}
}

func (n *Node) handleMsgReject(data []byte) {
	var reject Reject
	if err := json.Unmarshal(data, &reject); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки reject повідомлення")
		return
	}

	// Ігноруємо reject якщо він не відповідає нашому поточному стану
	if !bytes.Equal(reject.Proposer, n.nextProposer.Address) || reject.Round != n.currentRound {
		log.Debug().Msg("reject не відповідає поточному стану — ігнорується")
		return
	}

	log.Warn().
		Hex("proposer", reject.Proposer).
		Uint32("round", reject.Round).
		Msg("отримано reject — переходимо до наступного раунду")
	n.advanceRound()
}

// advanceRound збільшує номер раунду і вибирає нового proposer-а.
// Викликається коли поточний proposer зробив поганий блок або не відповів.
func (n *Node) advanceRound() {
	go drainChannel(n.vote)
	oldProposer := n.nextProposer.Address
	oldRound := n.currentRound
	n.currentRound++
	if err := n.setNextProposer(); err != nil {
		log.Error().Err(err).Msg("помилка вибору proposer після переходу раунду")
		return
	}
	log.Info().
		Hex("старий proposer", oldProposer).
		Uint32("старий раунд", oldRound).
		Hex("новий proposer", n.nextProposer.Address).
		Uint32("новий раунд", n.currentRound).
		Msg("перехід до наступного раунду")

	if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		go n.tryProposeBlock()
	}
}

// rejectCurrentProposer зберігає поточний (proposer, round) перед просуванням раунду,
// потім сповіщає мережу через MsgReject зі старими значеннями.
// Важливо зберегти старі значення ДО advanceRound, бо інакше повідомлення
// не відповідатиме стану інших нод.
func (n *Node) rejectCurrentProposer() {
	proposer := make([]byte, len(n.nextProposer.Address))
	copy(proposer, n.nextProposer.Address)
	round := n.currentRound

	n.advanceRound()

	msg, err := n.getRejectMsg(proposer, round)
	if err != nil {
		log.Error().Err(err).Msg("помилка створення reject повідомлення")
		return
	}
	if err = n.topic.broadcast(msg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка broadcast reject")
	}
}

func restartTimer(t *time.Timer, d time.Duration) (*time.Timer, <-chan time.Time) {
	if t != nil {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
	}
	nt := time.NewTimer(d)
	return nt, nt.C
}

func drainChannel[T any](ch chan T) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func containsInValidators(pub []byte, validators *[]chain.Validator) (bool, *chain.Validator) {
	for _, v := range *validators {
		if bytes.Equal(pub, v.Address) {
			return true, &v
		}
	}
	return false, nil
}
