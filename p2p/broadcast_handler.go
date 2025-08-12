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
			if message.Type != MsgBlockProposal {
				log.Debug().Msg("повідомлення від себе")
				continue
			}
			log.Debug().Msg("отримано власний блок")
		}

		if !message.verify() {
			log.Warn().Msg("підпис повідомлення not valid")
			continue
		}

		switch message.Type {
		case MsgNewTransaction:
			go n.handleMsgNewTransaction(message.Data)
		case MsgBlockProposal:
			go n.handleMsgBlockProposal(message.Data)
		case MsgVote:
			go n.handleMsgVote()
		case MsgCommit:
			go n.handleMsgCommit()
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

	// NOTE: я ще не впевнений в MsgVote, тому що, якщо я перевірив блок, і він правельний, то це означає, що усі за нього проголосують
	// TODO: додати перевірку автора ( щоб pubkey збігався з тим, хто повинен був робити блок ). І нагороду, яку він собі назначив
	lastLocalBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Fatal().Err(err).Msg("помилка отримання останнього блоку")
	}

	// Перевірка блоку
	if err = block.Verify(); err != nil {
		log.Err(err).Msg("помилка перевірки блоку")
		return
	}
	if err = block.VerifyTransactions(); err != nil {
		log.Err(err).Msg("транзакції блоку не є валідними")
		return
	}
	if lastLocalBlock.Height+1 != block.Height {
		log.Error().Uint32("висота отриманого блоку: ", block.Height).Uint32("очікувана висота", lastLocalBlock.Height+1).Msg("помилка висоти блоку")
		return
	}

	bytesBlock, err := json.Marshal(block)
	if err != nil {
		panic(err)
	}

	sig, err := crypto.Sign(n.keys.Priv, bytesBlock)
	if err != nil {
		panic(err)
	}

	msg := Message{
		Type:      MsgVote,
		Timestamp: time.Now().UnixMilli(),
		Data:      sig,
		Pub:       n.keys.Pub,
	}

	if err = msg.sign(n.keys.Priv); err != nil {
		panic(err)
	}

	if err = n.topic.broadcast(&msg, n.ctx); err != nil {
		panic(err)
	}

	// TODO: перенести в handleMsgCommit
	n.bs.SaveBlock(&block)

	// видалити транзакції з mempool, якщо вони є в блоці
	go func() {
		for _, tx := range block.Transactions {
			n.mempool.DeleteIfExist(tx)
		}
	}()

	for _, tx := range block.Transactions {
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
	/////////////////////////////////////////////////////////////

	// TODO: це повинно бути в handleMsgCommit
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
	/////////////////////////////////////////////////////////////
}

func (n *Node) handleMsgVote() {
}

func (n *Node) handleMsgCommit() {
}
