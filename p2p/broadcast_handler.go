package p2p

// TODO: зробити механізм відмови від блоку. коли блок не пройшов перевірку, треба щоб мережа не зупинялась, а вибрала іншого валідатора

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

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
		case MsgReject:
			n.handleMsgReject()
		}
	}
}

func (n *Node) handleMsgNewTransaction(data []byte) {
	var tx chain.Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки транзакції")
		return
	}

	log.Info().Int64("latency", time.Now().UnixMilli()-tx.Timestamp).Msg("отримано транзакцію")

	if err := n.mempool.Add(&tx); err != nil {
		log.Warn().Err(err).Msg("отримана транзакція не була додана до mempool")
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
		return
	}

	voteMsg, err := n.getVoteMsg(blockBytes)
	if err != nil {
		log.Error().Err(err).Msg("помилка створення повідомлення для голосування")
		return
	}

	if err = n.topic.broadcast(voteMsg, n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка розсилання повідомлення голосування")
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

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

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
		case <-timeout.C:
			log.Warn().Int64("зібрано", acceptedAmount).Int64("потрібно", stakeAmount/2+1).Msg("timeout очікування голосів — недостатньо голосів")
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
		log.Error().Int64("зібрано", acceptedStake).Int64("потрібно", totalStake/2+1).Msg("commit не має достатньої кількості голосів")
		return
	}

	if err := n.bs.SaveBlock(&commit.Block); err != nil {
		log.Error().Err(err).Msg("помилка збереження блоку")
		return
	}
	log.Info().Hex("block hash", commit.Block.Hash).Uint32("height", commit.Block.Height).Msg("додано новий блок до ланцюжка")

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

	if err := n.setNextProposer(); err != nil {
		log.Error().Err(err).Msg("помилка вибору наступного proposer")
		return
	}

	if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
		blockProposalMsg, err := n.getMsgBlockProposalMsg()
		if err != nil {
			log.Error().Err(err).Msg("помилка створення block proposal")
			return
		}

		if err = n.topic.broadcast(blockProposalMsg, n.ctx); err != nil {
			log.Error().Err(err).Msg("помилка трансляції нового блоку")
		}
	}
}

func (n *Node) handleMsgReject() {
	validator, err := n.bs.GetValidator(n.nextProposer.Address)
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання валідатора при reject")
		return
	}

	if err := n.bs.DeleteValidator(validator); err != nil {
		log.Error().Err(err).Msg("помилка видалення валідатора при reject")
		return
	}

	if err := n.setNextProposer(); err != nil {
		log.Error().Err(err).Msg("помилка вибору наступного proposer після reject")
	}
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
