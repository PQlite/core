package p2p

// TODO: зробити механізм відмови від блоку. коли блок не пройшов перевірку, треба щоб мережа не зупенялась, а вибрала іншого валідатора

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

// Читання вхідних повідомлень
func (n *Node) handleBroadcastMessages() {
	go n.processBlockProposalCommit()
	for {
		data, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Error().Err(err).Msg("помилка при отриманні повідомлення")
			continue
		}

		var message Message
		err = json.Unmarshal(data.Data, &message)
		if err != nil {
			log.Error().Err(err).Msg("помилка розпаковки повідомлення")
			continue
		}

		if data.ReceivedFrom == n.host.ID() {
			if message.Type != MsgBlockProposal && message.Type != MsgCommit && message.Type != MsgVote {
				log.Debug().Msg("повідомлення від себе")
				continue
			}
			log.Debug().Str("type", string(message.Type)).Msg("отримано власне повідомлення типу")
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

func (n *Node) processBlockProposalCommit() {
	for {
		message := <-n.messagesQueue

		switch message.Type {
		case MsgBlockProposal:
			n.handleMsgBlockProposal(message.Data)
		case MsgCommit:
			n.handleMsgCommit(message.Data)
		}
	}
}

func (n *Node) handleMsgNewTransaction(data []byte) {
	var tx chain.Transaction
	err := json.Unmarshal(data, &tx)
	if err != nil {
		log.Error().Err(err).Msg("помилка розпаковки транзакції")
		return
	}

	log.Info().Int64("latency", time.Now().UnixMilli()-tx.Timestamp).Msg("отримано транзакцію")

	if err = n.mempool.Add(&tx); err != nil {
		log.Warn().Err(err).Msg("отрмана транзакція не була додана до mempool")
	}
}

func (n *Node) handleMsgBlockProposal(data []byte) {
	var block chain.Block
	err := json.Unmarshal(data, &block)
	if err != nil {
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
		return
	}

	msg, err := n.getVoteMsg(blockBytes)
	if err != nil {
		log.Error().Err(err).Msg("помилка створення повідомлення для голосування")
		return
	}

	if err = n.topic.broadcast(msg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка розсилання повідомлення голосування")
		return
	}

	// якщо це не я роблю блок
	if !bytes.Equal(block.Proposer, n.keys.Pub) {
		return
	}

	// OPTIMIZE: я думаю зробити список, в якому будуть ставитись галочки чи щось таке
	// TODO: додати обробку сценарію, коли не проголосували в достатній кількості
	var votersList []chain.Vote
	allValidators, err := n.bs.GetValidatorsList()
	if err != nil {
		panic(err)
	}

	var accceptedAmount int64
	var stakeAmount int64
	for _, validator := range *allValidators {
		stakeAmount += validator.Amount
	}

	for {
		v := <-n.vote
		if err = crypto.Verify(v.Pub, blockBytes, v.Signature); err != nil {
			log.Info().Msg("голос не є вілідним")
			continue
		}

		contais, validator := containsInValidators(v.Pub, allValidators)
		if contais {
			accceptedAmount += validator.Amount

			votersList = append(votersList, v)
		}

		// NOTE: для релізу погано, але зараз ок
		if stakeAmount == accceptedAmount { // 100%
			break
		}
	}

	commitMsg, err := n.getCommitMsg(&votersList, &block)
	if err != nil {
		panic(err)
	}

	if err = n.topic.broadcast(commitMsg, n.ctx); err != nil {
		panic(err)
	}
	log.Debug().Msg("повідомлення commit відправлено")
}

func (n *Node) handleMsgVote(data []byte) {
	var vote chain.Vote
	if err := json.Unmarshal(data, &vote); err != nil {
		panic(err)
	}
	log.Debug().Hex("від", vote.Pub).Msg("отримано повідомлення  vote")
	n.vote <- vote
}

func (n *Node) handleMsgCommit(data []byte) {
	go drainChannel(n.vote)

	var commit Commit
	if err := json.Unmarshal(data, &commit); err != nil {
		panic(err)
	}

	// TODO: перевірити дані з commit
	allValidators, err := n.bs.GetValidatorsList()
	if err != nil {
		panic(err)
	}
	for _, v := range commit.Voters {
		if err := v.Verify(&commit.Block); err != nil {
			log.Error().Hex("voter", v.Pub).Msg("помилка підтвердження підпису голосу")
			return
		}

		contains, _ := containsInValidators(v.Pub, allValidators)
		if !contains {
			log.Error().Hex("voter", v.Pub).Msg("голос не був в списку валідаторів")
			return
		}
	}

	if err := n.bs.SaveBlock(&commit.Block); err != nil {
		panic(err)
	}
	log.Info().Hex("block hash", commit.Block.Hash).Uint32("height", commit.Block.Height).Msg("додано новий блок до лонцюжка")

	go n.mempool.ClearMempool(commit.Block.Transactions)

	if err := n.addValidatorsToDB(&commit.Block); err != nil {
		panic(err)
	}

	if err := n.updateBalancesNonces(&commit.Block); err != nil {
		panic(err)
	}

	if err := n.setNextProposer(); err != nil {
		panic(err)
	}

	// я і є настпуний валідатор!
	if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		blockProposalMsg, err := n.getMsgBlockProposalMsg()
		if err != nil {
			panic(err)
		}

		if err = n.topic.broadcast(blockProposalMsg, n.ctx); err != nil {
			log.Error().Err(err).Msg("помилка трансляції нового блоку")
		}
	}
}

func drainChannel[T any](ch chan T) {
	for {
		select {
		case <-ch:
			// просто читаємо і відкидаємо значення
		default:
			// канал порожній - виходимо
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
