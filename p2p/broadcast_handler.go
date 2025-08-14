package p2p

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
	// NOTE: думаю, що треба зробити функцію для кожного випадка і обробляти її типу: go handleMsgNewTransaction(), тому що коли буде багато вхідних повідомлень, воно може їх просто пропускати
	// або винести усю обробку в окрему gorutine, а n.topic.sub.Next буде кидати усі нові повідомлення в канал. таким чином можна зберегти послідовну обробку і не пропоскати повідомлення
	for {
		msg, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Error().Err(err).Msg("помилка при отриманні повідомлення")
			continue
		}

		var message Message
		err = json.Unmarshal(msg.Data, &message)
		if err != nil {
			log.Error().Err(err).Msg("помилка розпаковки повідомлення")
			continue
		}

		if msg.ReceivedFrom == n.host.ID() {
			if message.Type != MsgBlockProposal && message.Type != MsgCommit && message.Type != MsgVote {
				log.Debug().Msg("повідомлення від себе")
				continue
			}
			log.Debug().Msg("отримано власний блок")
		}

		if !message.verify() {
			log.Warn().Msg("підпис повідомлення not valid")
			continue
		}

		log.Info().Str("отримав мовідомлення", string(message.Type))

		switch message.Type {
		case MsgNewTransaction:
			go n.handleMsgNewTransaction(message.Data)
		case MsgBlockProposal:
			go n.handleMsgBlockProposal(message.Data)
		case MsgVote:
			go n.handleMsgVote(message.Data)
		case MsgCommit:
			go n.handleMsgCommit(message.Data)
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

	// TODO: додати перевірку нагороди, яку він собі назначив
	lastLocalBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Fatal().Err(err).Msg("помилка отримання останнього блоку")
	}

	// Перевірка блоку
	if err = block.Verify(); err != nil {
		log.Err(err).Msg("помилка перевірки блоку")
		return
	}
	// перевірка усіх транзакцій в блоці
	if err = block.VerifyTransactions(); err != nil {
		log.Err(err).Msg("транзакції блоку не є валідними")
		return
	}
	// чи правельна висота блоку який був отриманий (на один більше попереднього)
	if lastLocalBlock.Height+1 != block.Height {
		log.Error().Uint32("висота отриманого блоку", block.Height).Uint32("очікувана висота", lastLocalBlock.Height+1).Msg("помилка висоти блоку")
		return
	}
	// Чи правельний творець блоку
	if !bytes.Equal(block.Proposer, n.nextProposer.Address) {
		log.Error().Hex("адреса творця блоку", block.Proposer).Hex("адреса валідатора, який поминен робити блок", n.nextProposer.Address).Msg("адреса творця блоку і того хто повинен його робити не збігаются")
		return
	}

	bytesBlock, err := block.MarshalDeterministic()
	if err != nil {
		panic(err)
	}

	sig, err := crypto.Sign(n.keys.Priv, bytesBlock)
	if err != nil {
		panic(err)
	}

	vote := chain.Vote{
		Pub:       n.keys.Pub,
		Signature: sig,
	}

	voteBytes, err := json.Marshal(vote)
	if err != nil {
		panic(err)
	}

	msg := Message{
		Type:      MsgVote,
		Timestamp: time.Now().UnixMilli(),
		Data:      voteBytes,
		Pub:       n.keys.Pub,
	}

	if err = msg.sign(n.keys.Priv); err != nil {
		panic(err)
	}

	if err = n.topic.broadcast(&msg, n.ctx); err != nil {
		panic(err)
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

	var accceptedAmount float32
	var stakeAmount float32
	for _, validator := range *allValidators {
		stakeAmount += validator.Amount
	}

	for {
		v := <-n.vote
		if err = crypto.Verify(v.Pub, bytesBlock, v.Signature); err != nil {
			log.Info().Msg("голос не є вілідним")
			continue
		}

		votersList = append(votersList, v)

		for _, validator := range *allValidators {
			if bytes.Equal(v.Pub, validator.Address) {
				accceptedAmount += validator.Amount
			}
		}

		// NOTE: для релізу погано, але зараз ок
		if stakeAmount == accceptedAmount { // 100%
			break
		}
	}

	commit := Commit{
		Voters: votersList,
		Block:  block,
	}

	commitBytes, err := json.Marshal(commit)
	if err != nil {
		panic(err)
	}

	commitMsg := Message{
		Type:      MsgCommit,
		Timestamp: time.Now().UnixMilli(),
		Data:      commitBytes,
		Pub:       n.keys.Pub,
	}
	if err = commitMsg.sign(n.keys.Priv); err != nil {
		panic(err)
	}

	if err = n.topic.broadcast(&commitMsg, n.ctx); err != nil {
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

	// allValidators, err := n.bs.GetValidatorsList()
	// if err != nil {
	// 	panic(err)
	// }
	// for _, v := range commit.Voters {
	// 	v.
	//
	// }

	n.bs.SaveBlock(&commit.Block)
	log.Info().Hex("block hash", commit.Block.Hash).Uint32("height", commit.Block.Height).Msg("додано новий блок до лонцюжка")

	// видалити транзакції з mempool, якщо вони є в блоці
	go func() {
		for _, tx := range commit.Block.Transactions {
			n.mempool.DeleteIfExist(tx)
		}
	}()

	for _, tx := range commit.Block.Transactions {
		if bytes.Equal(tx.To, []byte(STAKE)) {
			validator := chain.Validator{
				Address: tx.PubKey, // NOTE: досі не вирішив, чи я використовую hash або pub
				Amount:  tx.Amount,
			}

			n.bs.AddValidator(&validator)
		}
	}

	val, err := n.chooseValidator()
	if err != nil {
		log.Error().Err(err).Msg("помилка вибору наступного валідатора")
		return
	}
	n.nextProposer = val

	// я і є настпуний валідатор!
	if bytes.Equal(val.Address, n.keys.Pub) {
		newBlock := n.createNewBlock()

		newBlockBytes, err := json.Marshal(newBlock)
		if err != nil {
			log.Fatal().Err(err).Msg("помилка розпаковки нового блоку")
		}

		blockProposalMsg := Message{
			Type:      MsgBlockProposal,
			Timestamp: time.Now().UnixMilli(),
			Data:      newBlockBytes,
			Pub:       n.keys.Pub,
		}

		err = blockProposalMsg.sign(n.keys.Priv)
		if err != nil {
			log.Fatal().Err(err).Msg("помилка підпису нового блоку")
		}

		err = n.topic.broadcast(&blockProposalMsg, n.ctx)
		if err != nil {
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
