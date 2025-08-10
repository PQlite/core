package p2p

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/rs/zerolog/log"
)

// Читання вхідних повідомлень
func (n *Node) handleBroadcastMessages() {
	for {
		msg, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Error().Err(err).Msg("помилка при отриманні повідомлення")
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
			var tx chain.Transaction
			err = json.Unmarshal(message.Data, &tx)
			if err != nil {
				log.Error().Err(err).Msg("помилка розпаковки транзакції")
				continue
			}

			err = n.mempool.Add(&tx)
			if err != nil {
				log.Warn().Err(err).Msg("отрмана транзакція не була додана до mempool")
			}
		case MsgBlockProposal:
			var block chain.Block
			err = json.Unmarshal(message.Data, &block)
			if err != nil {
				log.Error().Err(err).Msg("помилка розпаковки blockProposal")
				continue
			}
			log.Info().Uint32("height", block.Height).Msg("отримано новий блок")

			// NOTE: я ще не впевнений в MsgVote, тому що, якщо я перевірив блок, і він правельний, то це означає, що усі за нього проголосують
			// TODO: додати перевірку автора ( щоб pubkey збігався з тим, хто повинен був робити блок ). І нагороду, яку він собі назначив
			// TODO: видалити транзакції з mempool, якщо вони вже є в блоці
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
			n.bs.SaveBlock(&block)

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
				continue
			}

			// я це і є настпуний валідатор!
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
			continue
		}

		latency := time.Now().UnixMilli() - message.Timestamp
		log.Info().Int64("latency", latency).Msg("oтримано")
	}
}
